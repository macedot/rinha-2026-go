package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"rinha-2026/internal/config"
	"rinha-2026/internal/httpresp"
	"rinha-2026/internal/ivfsearch"
	"rinha-2026/internal/vectorizer"
	"runtime"
	"unsafe"

	"go.uber.org/automaxprocs/maxprocs"
	"golang.org/x/sys/unix"
)

const (
	reqBufSize = 8192
	maxEvents  = 128
	maxCtrl    = 4
)

var cfg *config.Config

type conn struct {
	fd     int
	buf    [reqBufSize]byte
	bufLen int
	active bool
}

var conns [maxEvents]conn

func connNew(fd int) *conn {
	for i := range conns {
		if !conns[i].active {
			conns[i].fd = fd
			conns[i].bufLen = 0
			conns[i].active = true
			return &conns[i]
		}
	}
	return nil
}

func connClose(c *conn) {
	if c.fd >= 0 {
		unix.Close(c.fd)
		c.fd = -1
	}
	c.active = false
	c.bufLen = 0
}

func sendAll(fd int, data []byte) bool {
	sent := 0
	for sent < len(data) {
		n, err := unix.Write(fd, data[sent:])
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				pfds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
				unix.Poll(pfds, 1000)
				continue
			}
			return false
		}
		if n == 0 {
			return false
		}
		sent += n
	}
	return true
}

func findContentLength(headers []byte) int {
	key := []byte("Content-Length:")
	idx := bytes.Index(headers, key)
	if idx < 0 {
		return -1
	}
	p := idx + len(key)
	for p < len(headers) && (headers[p] == ' ' || headers[p] == '\t') {
		p++
	}
	v := 0
	for p < len(headers) && headers[p] >= '0' && headers[p] <= '9' {
		v = v*10 + int(headers[p]-'0')
		p++
	}
	return v
}

func handleRequest(c *conn) int {
	reqBuf := c.buf[:c.bufLen]
	fd := c.fd

	hdrEnd := bytes.Index(reqBuf, []byte("\r\n\r\n"))
	if hdrEnd < 0 {
		if c.bufLen >= reqBufSize-1 {
			return -1
		}
		return 0
	}

	headerLen := hdrEnd + 4

	if len(reqBuf) >= 10 && string(reqBuf[:10]) == "GET /ready" {
		if !sendAll(fd, httpresp.Ready) {
			return -1
		}
		c.bufLen = 0
		return 1
	}

	if len(reqBuf) >= 9 && string(reqBuf[:9]) == "GET /inst" {
		sendAll(fd, []byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\ninst disabled\n"))
		return -1
	}

	if len(reqBuf) >= 17 && string(reqBuf[:17]) == "POST /fraud-score" {
		cl := findContentLength(reqBuf[:hdrEnd])
		if cl < 0 {
			if !sendAll(fd, httpresp.BadRequest) {
				return -1
			}
			c.bufLen = 0
			return 1
		}
		if c.bufLen < headerLen+cl {
			if c.bufLen+cl >= reqBufSize {
				return -1
			}
			return 0
		}

		body := reqBuf[headerLen : headerLen+cl]
		q, ok := vectorizer.Build(body)
		if !ok {
			if !sendAll(fd, httpresp.BadRequest) {
				return -1
			}
			c.bufLen = 0
			return 1
		}

		votes := ivfsearch.Search(q)
		if votes < 0 || votes > 5 {
			if !sendAll(fd, httpresp.InternalError) {
				return -1
			}
			c.bufLen = 0
			return 1
		}
		if !sendAll(fd, httpresp.ScoreBody[votes]) {
			return -1
		}

		consumed := headerLen + cl
		if c.bufLen > consumed {
			copy(c.buf[:], c.buf[consumed:c.bufLen])
			c.bufLen -= consumed
			return 2
		}
		c.bufLen = 0
		return 1
	}

	if !sendAll(fd, httpresp.NotFound) {
		return -1
	}
	c.bufLen = 0
	return 1
}

func processConn(c *conn) int {
	fd := c.fd
	didRecv := false

	for {
		if c.bufLen == 0 || !didRecv {
			n, err := unix.Read(fd, c.buf[c.bufLen:reqBufSize-c.bufLen])
			if err != nil {
				if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
					return 0
				}
				return -1
			}
			if n == 0 {
				return -1
			}
			c.bufLen += n
			didRecv = true
		}

		rc := handleRequest(c)
		if rc < 0 {
			return -1
		}
		if rc == 0 {
			return 0
		}

		if rc == 2 {
			didRecv = false
			continue
		}

		didRecv = false
	}
}

func maybePinCPU(udsPath string) {
	var set unix.CPUSet
	if err := unix.SchedGetaffinity(0, &set); err != nil {
		return
	}
	ncpu := set.Count()
	fmt.Fprintf(os.Stderr, "available CPUs: %d\n", ncpu)

	var h uint64 = 5381
	for i := 0; i < len(udsPath); i++ {
		h = ((h << 5) + h) + uint64(udsPath[i])
	}
	core := int(h % uint64(ncpu))

	var pin unix.CPUSet
	pin.Zero()
	pin.Set(core)

	runtime.LockOSThread()
	if err := unix.SchedSetaffinity(0, &pin); err == nil {
		fmt.Fprintf(os.Stderr, "pinned to CPU %d\n", core)
	} else {
		fmt.Fprintf(os.Stderr, "pin to CPU %d failed: %v\n", core, err)
	}
}

func main() {
	if _, err := maxprocs.Set(); err != nil {
		log.Printf("automaxprocs: %v", err)
	}
	cfg = config.Load()
	httpresp.Init()

	maybePinCPU(cfg.UDSPath)

	if err := ivfsearch.LoadIndex(cfg.IndexPath); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load INDEX_PATH=%s\n", cfg.IndexPath)
		log.Fatal(err)
	}
	ivfsearch.SetParams(cfg.IvfNprobe, cfg.IvfFullNprobe, cfg.Candidates)

	fmt.Fprintf(os.Stderr, "engine: pure Go IVF/kmeans + int16 + top5 seco (K=4096)\n")

	{
		var state uint32 = 0x12345678
		for i := 0; i < 5000; i++ {
			var q [14]float32
			for j := 0; j < 14; j++ {
				state = state*1664525 + 1013904223
				q[j] = float32(state>>8) / float32(1<<24)
			}
			ivfsearch.Search(q)
		}
	}
	fmt.Fprintf(os.Stderr, "cache warmup done\n")

	if cfg.UnlinkUDS {
		os.Remove(cfg.UDSPath)
		os.Remove(cfg.UDSPath + ".ctrl")
	}

	mainFd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		log.Fatalf("socket main: %v", err)
	}
	defer unix.Close(mainFd)

	mainAddr := &unix.SockaddrUnix{Name: cfg.UDSPath}
	if err := unix.Bind(mainFd, mainAddr); err != nil {
		log.Fatalf("bind main: %v", err)
	}
	if err := unix.Listen(mainFd, 1024); err != nil {
		log.Fatalf("listen main: %v", err)
	}
	if cfg.UDSMode > 0 {
		os.Chmod(cfg.UDSPath, os.FileMode(octalFromDecimal(cfg.UDSMode)))
	}
	fmt.Fprintf(os.Stderr, "listening UDS %s mode=%d\n", cfg.UDSPath, cfg.UDSMode)

	ctrlPath := cfg.UDSPath + ".ctrl"
	ctrlFd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		log.Fatalf("socket ctrl: %v", err)
	}
	defer unix.Close(ctrlFd)

	ctrlAddr := &unix.SockaddrUnix{Name: ctrlPath}
	if err := unix.Bind(ctrlFd, ctrlAddr); err != nil {
		log.Fatalf("bind ctrl: %v", err)
	}
	if err := unix.Listen(ctrlFd, 1024); err != nil {
		log.Fatalf("listen ctrl: %v", err)
	}
	if cfg.UDSMode > 0 {
		os.Chmod(ctrlPath, os.FileMode(octalFromDecimal(cfg.UDSMode)))
	}
	fmt.Fprintf(os.Stderr, "listening ctrl %s\n", ctrlPath)

	epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		log.Fatalf("epoll_create1: %v", err)
	}

	evMain := &unix.EpollEvent{Events: unix.EPOLLIN, Fd: int32(mainFd)}
	if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, mainFd, evMain); err != nil {
		log.Fatalf("epoll_ctl main: %v", err)
	}
	evCtrl := &unix.EpollEvent{Events: unix.EPOLLIN, Fd: int32(ctrlFd)}
	if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, ctrlFd, evCtrl); err != nil {
		log.Fatalf("epoll_ctl ctrl: %v", err)
	}

	events := make([]unix.EpollEvent, maxEvents)

	var ctrlConns [maxCtrl]int
	ctrlCount := 0

	ctrlConnAdd := func(fd int) int {
		if ctrlCount >= maxCtrl {
			return -1
		}
		ctrlConns[ctrlCount] = fd
		ctrlCount++
		return ctrlCount - 1
	}

	ctrlConnRemove := func(idx int) {
		if idx < 0 || idx >= ctrlCount {
			return
		}
		unix.Close(ctrlConns[idx])
		for i := idx; i < ctrlCount-1; i++ {
			ctrlConns[i] = ctrlConns[i+1]
		}
		ctrlCount--
	}

	ctrlConnFind := func(fd int) int {
		for i := 0; i < ctrlCount; i++ {
			if ctrlConns[i] == fd {
				return i
			}
		}
		return -1
	}

	recvFD := func(connFd int) (int, error) {
		var cmsgBuf [24]byte
		var dummy byte
		iovec := []unix.Iovec{{Base: &dummy, Len: 1}}
		msghdr := unix.Msghdr{
			Iov:     &iovec[0],
			Iovlen:  1,
			Control: (*byte)(unsafe.Pointer(&cmsgBuf[0])),
		}
		msghdr.Controllen = uint64(len(cmsgBuf))
		_, _, e := unix.Syscall(unix.SYS_RECVMSG, uintptr(connFd), uintptr(unsafe.Pointer(&msghdr)), 0)
		if e != 0 {
			return -1, e
		}
		cmsgs, e2 := unix.ParseSocketControlMessage(cmsgBuf[:msghdr.Controllen])
		if e2 != nil {
			return -1, e2
		}
		for _, cmsg := range cmsgs {
			if cmsg.Header.Level == unix.SOL_SOCKET && cmsg.Header.Type == unix.SCM_RIGHTS {
				fds, _ := unix.ParseUnixRights(&cmsg)
				if len(fds) > 0 {
					return fds[0], nil
				}
			}
		}
		return -1, unix.EINVAL
	}

	for {
		n, err := unix.EpollWait(epfd, events, -1)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			log.Fatalf("epoll_wait: %v", err)
		}

		for i := 0; i < n; i++ {
			fd := int(events[i].Fd)

			if fd == ctrlFd {
				for {
					connFd, _, e := unix.Accept(ctrlFd)
					if e != nil {
						break
					}
					unix.SetNonblock(connFd, true)
					idx := ctrlConnAdd(connFd)
					if idx < 0 {
						unix.Close(connFd)
						continue
					}
					cev := &unix.EpollEvent{Events: unix.EPOLLIN, Fd: int32(connFd)}
					if unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, connFd, cev) != nil {
						ctrlConnRemove(idx)
						continue
					}
				}
			} else if fd == mainFd {
				for {
					connFd, _, e := unix.Accept(mainFd)
					if e != nil {
						break
					}
					unix.SetNonblock(connFd, true)
					c := connNew(connFd)
					if c == nil {
						unix.Close(connFd)
						continue
					}
					cev := &unix.EpollEvent{Events: unix.EPOLLIN | unix.EPOLLET, Fd: int32(connFd)}
					if unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, connFd, cev) != nil {
						connClose(c)
						continue
					}
				}
			} else if ctrlIdx := ctrlConnFind(fd); ctrlIdx >= 0 {
				for {
					clientFd, recvErr := recvFD(fd)
					if recvErr != nil {
						if recvErr == unix.EAGAIN || recvErr == unix.EWOULDBLOCK {
							break
						}
						unix.EpollCtl(epfd, unix.EPOLL_CTL_DEL, fd, nil)
						ctrlConnRemove(ctrlIdx)
						break
					}

					unix.SetNonblock(clientFd, true)
					unix.SetsockoptInt(clientFd, unix.IPPROTO_TCP, unix.TCP_NODELAY, 1)
					unix.SetsockoptInt(clientFd, unix.IPPROTO_TCP, unix.TCP_QUICKACK, 1)

					c := connNew(clientFd)
					if c == nil {
						unix.Close(clientFd)
						continue
					}
					cev := &unix.EpollEvent{Events: unix.EPOLLIN | unix.EPOLLET, Fd: int32(clientFd)}
					if unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, clientFd, cev) != nil {
						connClose(c)
						continue
					}
				}
			} else {
				var c *conn
				for j := range conns {
					if conns[j].active && conns[j].fd == fd {
						c = &conns[j]
						break
					}
				}
				if c != nil {
					rc := processConn(c)
					if rc < 0 {
						unix.EpollCtl(epfd, unix.EPOLL_CTL_DEL, c.fd, nil)
						connClose(c)
					}
				} else {
					unix.EpollCtl(epfd, unix.EPOLL_CTL_DEL, fd, nil)
					unix.Close(fd)
				}
			}
		}
	}
}

func octalFromDecimal(mode int) int {
	a := mode / 100
	b := (mode / 10) % 10
	c := mode % 10
	return (a << 6) | (b << 3) | c
}
