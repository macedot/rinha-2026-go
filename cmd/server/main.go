package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"rinha-2026/internal/config"
	"rinha-2026/internal/httpresp"
	"rinha-2026/internal/ivfsearch"
	"rinha-2026/internal/mccrisk"
	"rinha-2026/internal/vectorizer"
	"syscall"
	"unsafe"

	"github.com/valyala/fasthttp"
	"go.uber.org/automaxprocs/maxprocs"
)

var (
	cfg      *config.Config
	mccTable *mccrisk.Table
)

func main() {
	if _, err := maxprocs.Set(); err != nil {
		log.Printf("automaxprocs: %v", err)
	}
	cfg = config.Load()
	httpresp.Init()
	mccTable = mccrisk.Load(cfg.MccRiskPath)

	if err := ivfsearch.BridgeLoadIndex(cfg.IndexPath); err != nil {
		fmt.Fprintf(os.Stderr, "falha carregando INDEX_PATH=%s\n", cfg.IndexPath)
		log.Fatal(err)
	}
	ivfsearch.BridgeSetParams(cfg.IvfNprobe, cfg.IvfFullNprobe, cfg.Candidates)

	/* Warm CPU caches with random queries */
	fmt.Fprintf(os.Stderr, "warming caches...\n")
	for i := 0; i < 500; i++ {
		q := [14]float32{
			float32(i%10000) / 10000.0, float32(i%12) / 12.0,
			float32(i%10) / 10.0, float32(i%24) / 23.0, float32(i%7) / 6.0,
			-1.0, -1.0, float32(i%1000) / 1000.0, float32(i%20) / 20.0,
			0.0, 1.0, 1.0, 0.5, float32(i%10000) / 10000.0,
		}
		ivfsearch.BridgeSearch(q)
	}
	fmt.Fprintf(os.Stderr, "cache warmup done\n")

	fmt.Fprintf(os.Stderr, "engine: IVF/kmeans + int16 + top5 seco + C/AVX2 bridge (K=4096)\n")

	s := &fasthttp.Server{
		Handler:               handler,
		MaxRequestBodySize:    32 * 1024,
		ReadBufferSize:        16 * 1024,
		WriteBufferSize:       16 * 1024,
		DisableKeepalive:      true,
		TCPKeepalive:          true,
		ReadTimeout:           0,
		WriteTimeout:          0,
		IdleTimeout:           60,
		NoDefaultServerHeader: true,
		ReduceMemoryUsage:     false,
	}

	if cfg.UnlinkUDS {
		os.Remove(cfg.UDSPath)
	}

	/* Main UDS socket for health checks / direct connections */
	mainLn, err := net.Listen("unix", cfg.UDSPath)
	if err != nil {
		log.Fatalf("listen uds: %v", err)
	}
	defer os.Remove(cfg.UDSPath)
	if cfg.UDSMode > 0 {
		mode := octalFromDecimal(cfg.UDSMode)
		os.Chmod(cfg.UDSPath, os.FileMode(mode))
	}
	fmt.Fprintf(os.Stderr, "listening UDS %s mode=%d\n", cfg.UDSPath, cfg.UDSMode)

	/* SCM_RIGHTS ctrl socket for SoNoForevis LB fd passing */
	ctrlLn, err := NewSCMRightsListener(cfg.UDSPath)
	if err != nil {
		log.Fatalf("listen ctrl: %v", err)
	}
	defer ctrlLn.Close()
	fmt.Fprintf(os.Stderr, "listening ctrl %s.ctrl\n", cfg.UDSPath)

	/* Serve on both listeners concurrently */
	go func() {
		log.Fatal(s.Serve(mainLn))
	}()
	log.Fatal(s.Serve(ctrlLn))
}

func handler(ctx *fasthttp.RequestCtx) {
	switch string(ctx.Path()) {
	case "/ready":
		ctx.SetStatusCode(200)
		ctx.Response.SetBodyRaw(httpresp.Ready)
		return
	case "/fraud-score":
		if !ctx.IsPost() {
			ctx.SetStatusCode(404)
			ctx.Response.SetBodyRaw(httpresp.NotFound)
			ctx.SetConnectionClose()
			return
		}
		body := ctx.PostBody()
		if len(body) == 0 {
			ctx.SetStatusCode(400)
			ctx.SetContentType("application/json")
			ctx.Response.SetBodyRaw(httpresp.BadRequest)
			ctx.SetConnectionClose()
			return
		}
		q, ok := vectorizer.Build(body, cfg, mccTable)
		if !ok {
			ctx.SetStatusCode(400)
			ctx.SetContentType("application/json")
			ctx.Response.SetBodyRaw(httpresp.BadRequest)
			ctx.SetConnectionClose()
			return
		}
		votes := ivfsearch.BridgeSearch(q)
		if votes < 0 || votes > 5 {
			ctx.SetStatusCode(500)
			ctx.Response.SetBodyRaw(httpresp.InternalError)
			ctx.SetConnectionClose()
			return
		}
		ctx.SetStatusCode(200)
		ctx.SetContentType("application/json")
		ctx.Response.SetBodyRaw(httpresp.ScoreBody[votes])
		ctx.SetConnectionClose()
		return
	default:
		ctx.SetStatusCode(404)
		ctx.Response.SetBodyRaw(httpresp.NotFound)
		ctx.SetConnectionClose()
	}
}

func octalFromDecimal(mode int) int {
	a := mode / 100
	b := (mode / 10) % 10
	c := mode % 10
	return (a << 6) | (b << 3) | c
}

/* === SCM_RIGHTS listener for SoNoForevis v1.0.0 === */

type SCMRightsListener struct {
	ctrlPath string
	ctrlLn   net.Listener
}

func NewSCMRightsListener(basePath string) (*SCMRightsListener, error) {
	ctrlPath := basePath + ".ctrl"
	os.Remove(ctrlPath)
	ln, err := net.Listen("unix", ctrlPath)
	if err != nil {
		return nil, fmt.Errorf("listen ctrl: %w", err)
	}
	return &SCMRightsListener{ctrlPath: ctrlPath, ctrlLn: ln}, nil
}

func (l *SCMRightsListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.ctrlLn.Accept()
		if err != nil {
			return nil, err
		}
		fd, err := recvFD(conn)
		if err != nil {
			conn.Close()
			continue
		}
		conn.Close()

		f := os.NewFile(uintptr(fd), "scmd")
		netConn, err := net.FileConn(f)
		f.Close()
		if err != nil {
			syscall.Close(fd)
			continue
		}
		return netConn, nil
	}
}

func (l *SCMRightsListener) Close() error {
	os.Remove(l.ctrlPath)
	return l.ctrlLn.Close()
}

func (l *SCMRightsListener) Addr() net.Addr {
	return l.ctrlLn.Addr()
}

func recvFD(conn net.Conn) (int, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return -1, fmt.Errorf("not a unix conn")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return -1, err
	}

	var fd int
	var recvErr error
	raw.Control(func(f uintptr) {
		fd, recvErr = recvFDRaw(int(f))
	})
	if recvErr != nil {
		return -1, recvErr
	}
	return fd, nil
}

func recvFDRaw(sockfd int) (int, error) {
	buf := make([]byte, syscall.CmsgSpace(4))
	var dummy byte
	iov := syscall.Iovec{Base: &dummy, Len: 1}
	msg := syscall.Msghdr{
		Iov:        &iov,
		Iovlen:     1,
		Control:    &buf[0],
		Controllen: uint64(len(buf)),
	}

	_, _, errno := syscall.Syscall(syscall.SYS_RECVMSG, uintptr(sockfd), uintptr(unsafe.Pointer(&msg)), 0)
	if errno != 0 {
		return -1, errno
	}

	cmsgs, err := syscall.ParseSocketControlMessage(buf[:msg.Controllen])
	if err != nil {
		return -1, err
	}

	for _, cmsg := range cmsgs {
		if cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == syscall.SCM_RIGHTS {
			fds, err := syscall.ParseUnixRights(&cmsg)
			if err != nil {
				return -1, err
			}
			if len(fds) > 0 {
				return fds[0], nil
			}
		}
	}
	return -1, fmt.Errorf("no fd received")
}
