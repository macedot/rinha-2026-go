package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"rinha-2026/internal/config"
	"rinha-2026/internal/httpresp"
	"rinha-2026/internal/ivfsearch"
	"rinha-2026/internal/vectorizer"
	"syscall"
	"unsafe"

	"github.com/valyala/fasthttp"
	"go.uber.org/automaxprocs/maxprocs"
)

var cfg *config.Config

func main() {
	if _, err := maxprocs.Set(); err != nil {
		log.Printf("automaxprocs: %v", err)
	}
	cfg = config.Load()
	httpresp.Init()

	if err := ivfsearch.LoadIndex(cfg.IndexPath); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load INDEX_PATH=%s\n", cfg.IndexPath)
		log.Fatal(err)
	}
	ivfsearch.SetParams(cfg.IvfNprobe, cfg.IvfFullNprobe, cfg.Candidates)

	fmt.Fprintf(os.Stderr, "engine: pure Go IVF/kmeans + int16 + top5 seco (K=4096)\n")

	// Warm CPU caches
	{
		var state uint32 = 0x12345678
		for i := 0; i < 500; i++ {
			var q [14]float32
			for j := 0; j < 14; j++ {
				state = state*1664525 + 1013904223
				q[j] = float32(state>>8) / float32(1<<24)
			}
			ivfsearch.Search(q)
		}
	}
	fmt.Fprintf(os.Stderr, "cache warmup done\n")

	s := &fasthttp.Server{
		Handler:               handler,
		MaxRequestBodySize:    32 * 1024,
		ReadBufferSize:        16 * 1024,
		WriteBufferSize:       16 * 1024,
		DisableKeepalive:      false,
		TCPKeepalive:          false,
		ReadTimeout:           0,
		WriteTimeout:          0,
		IdleTimeout:           0,
		NoDefaultServerHeader: true,
		ReduceMemoryUsage:     false,
	}

	if cfg.UnlinkUDS {
		os.Remove(cfg.UDSPath)
	}

	// Main UDS socket for health checks / direct connections
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

	// SCM_RIGHTS ctrl socket for SoNoForevis LB fd passing
	ctrlLn, err := NewSCMRightsListener(cfg.UDSPath)
	if err != nil {
		log.Fatalf("listen ctrl: %v", err)
	}
	defer ctrlLn.Close()
	fmt.Fprintf(os.Stderr, "listening ctrl %s.ctrl\n", cfg.UDSPath)

	// Serve on both listeners concurrently
	go func() {
		log.Fatal(s.Serve(mainLn))
	}()
	log.Fatal(s.Serve(ctrlLn))
}

func handler(ctx *fasthttp.RequestCtx) {
	path := ctx.Path()
	if len(path) == 6 && path[0] == '/' && path[1] == 'r' && path[2] == 'e' && path[3] == 'a' && path[4] == 'd' && path[5] == 'y' {
		ctx.SetStatusCode(200)
		ctx.Response.SetBodyRaw(httpresp.Ready)
		return
	}
	// /fraud-score is 12 bytes
	if len(path) == 12 && path[0] == '/' && path[6] == '-' && path[11] == 'e' {
		// /fraud-score
		body := ctx.PostBody()
		if len(body) == 0 {
			ctx.SetStatusCode(400)
			ctx.SetContentType("application/json")
			ctx.Response.SetBodyRaw(httpresp.BadRequest)
			return
		}
		q, ok := vectorizer.Build(body)
		if !ok {
			ctx.SetStatusCode(400)
			ctx.SetContentType("application/json")
			ctx.Response.SetBodyRaw(httpresp.BadRequest)
			return
		}
		votes := ivfsearch.Search(q)
		if votes < 0 || votes > 5 {
			ctx.SetStatusCode(500)
			ctx.Response.SetBodyRaw(httpresp.InternalError)
			return
		}
		ctx.SetStatusCode(200)
		ctx.SetContentType("application/json")
		ctx.Response.SetBodyRaw(httpresp.ScoreBody[votes])
		return
	}
	ctx.SetStatusCode(404)
	ctx.Response.SetBodyRaw(httpresp.NotFound)
}

func octalFromDecimal(mode int) int {
	a := mode / 100
	b := (mode / 10) % 10
	c := mode % 10
	return (a << 6) | (b << 3) | c
}

// === SCM_RIGHTS listener for SoNoForevis v1.0.0 ===

type SCMRightsListener struct {
	ctrlPath string
	ctrlLn   net.Listener
	ctrlConn net.Conn
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
		// If no active ctrl connection, accept one
		if l.ctrlConn == nil {
			conn, err := l.ctrlLn.Accept()
			if err != nil {
				return nil, err
			}
			l.ctrlConn = conn
		}

		// Try to receive an FD from the active ctrl connection
		fd, err := recvFD(l.ctrlConn)
		if err != nil {
			// Ctrl connection closed or error, accept a new one next time
			l.ctrlConn.Close()
			l.ctrlConn = nil
			continue
		}

		// Set non-blocking (required for fasthttp)
		syscall.SetNonblock(fd, true)

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
		// Make blocking for recvmsg
		syscall.SetNonblock(int(f), false)
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
