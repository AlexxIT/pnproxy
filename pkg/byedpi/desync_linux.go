//go:build linux

package byedpi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// WriteFakeMD5 sends the fake prefix of p with TCP_MD5SIG enabled, waits until
// TCP_INFO reports no unsent bytes, restores the mmap contents and socket
// options, then writes the final byte normally. Page restoration follows
// ByeDPI's Linux-specific technique. An empty unsent queue does not guarantee
// that the kernel has finished using gifted pages for retransmissions, so this
// behavior depends on the Linux networking implementation.
// The caller must not write to c concurrently or change its socket options.
// If an error occurs after bytes were queued, the connection is closed before
// restoring gifted pages so the failed payload cannot be replayed.
func WriteFakeMD5(c *net.TCPConn, p []byte) (sent int, err error) {
	if c == nil {
		return 0, net.ErrClosed
	}
	if len(p) == 0 {
		return 0, nil
	}
	if len(p) == 1 {
		return c.Write(p)
	}
	raw, e := c.SyscallConn()
	if e != nil {
		return 0, e
	}
	end := time.Now().Add(5 * time.Second)
	// RawConn.Write checks Go's existing write deadline without changing it.
	step := func(fn func(int) error) error {
		if time.Now().After(end) {
			return os.ErrDeadlineExceeded
		}
		var se error
		re := raw.Write(func(fd uintptr) bool { se = fn(int(fd)); return true })
		return errors.Join(re, se)
	}
	split := len(p) - 1
	page := unix.Getpagesize()
	mem, e := unix.Mmap(-1, 0, (split+page-1)/page*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if e != nil {
		return 0, e
	}
	defer func() { err = errors.Join(err, unix.Munmap(mem)) }()
	fake := selectFake(p)
	copy(mem[:split], fake) // mmap zero padding matches get_tcp_fake().
	var sig unix.TCPMD5Sig
	var level, opt, ttl int
	enabled, ttlChanged := false, false
	restored := false
	// Always restore the payload before removing MD5, including error paths.
	restore := func() error {
		copy(mem[:split], p[:split])
		var se error
		re := raw.Control(func(fd uintptr) {
			if enabled {
				sig.Keylen = 0
				se = unix.SetsockoptTCPMD5Sig(int(fd), unix.IPPROTO_TCP, unix.TCP_MD5SIG, &sig)
			}
			if ttlChanged {
				se = errors.Join(se, unix.SetsockoptInt(int(fd), level, opt, ttl))
			}
		})
		return errors.Join(re, se)
	}
	defer func() {
		if err != nil && (sent > 0 || enabled) {
			_ = c.Close()
			copy(mem[:split], p[:split])
		} else if !restored {
			err = errors.Join(err, restore())
		}
	}()
	e = step(func(fd int) error {
		// Use getpeername rather than RemoteAddr: IPv4-mapped IPv6 sockets must
		// retain their actual sockaddr family.
		size := uint32(unsafe.Sizeof(sig.Addr))
		_, _, errno := unix.Syscall(unix.SYS_GETPEERNAME, uintptr(fd), uintptr(unsafe.Pointer(&sig.Addr)), uintptr(unsafe.Pointer(&size)))
		if errno != 0 {
			return errno
		}
		addr := unsafe.Slice((*byte)(unsafe.Pointer(&sig.Addr)), int(size))
		level, opt = unix.IPPROTO_IP, unix.IP_TTL
		if binary.NativeEndian.Uint16(addr) == unix.AF_INET6 {
			level, opt = unix.IPPROTO_IPV6, unix.IPV6_UNICAST_HOPS
		}
		var x error
		ttl, x = unix.GetsockoptInt(fd, level, opt)
		if x != nil {
			return x
		}
		if x = queueEmpty(fd); x != nil {
			return x
		}
		if x = unix.SetsockoptInt(fd, level, opt, 8); x != nil {
			return x
		}
		ttlChanged = true
		sig.Keylen = 5 // ByeDPI uses five zero key bytes.
		if x = unix.SetsockoptTCPMD5Sig(fd, unix.IPPROTO_TCP, unix.TCP_MD5SIG, &sig); x != nil {
			return fmt.Errorf("%w: TCP_MD5SIG: %w", ErrUnsupported, x)
		}
		enabled = true
		return nil
	})
	if e != nil {
		return 0, e
	}
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	retry := func(fn func(int) error) error {
		for {
			x := step(fn)
			if x == nil {
				return nil
			}
			if !errors.Is(x, unix.EAGAIN) && !errors.Is(x, unix.EINTR) {
				return x
			}
			<-tick.C
		}
	}
	for off := 0; off < split; off += page {
		want := min(page, split-off)
		e = giftPage(mem[off:off+page], want, func(fd int, left int) (int, error) {
			moved := 0
			x := retry(func(sock int) error {
				var se error
				var n int64
				n, se = unix.Splice(fd, nil, sock, nil, left, unix.SPLICE_F_NONBLOCK)
				moved = int(n)
				return se
			})
			if moved > 0 {
				sent += moved
			}
			return moved, x
		})
		if e != nil {
			return sent, e
		}
	}
	if e = retry(queueEmpty); e != nil {
		return sent, e
	}
	if e = restore(); e != nil {
		return sent, e
	}
	restored = true
	// ByeDPI restores the pages once TCP_INFO reports no unsent bytes. Waiting
	// for ACKs here would hang when the decoy is deliberately rejected.
	n, e := c.Write(p[split:])
	return sent + n, e
}

func giftPage(p []byte, want int, drain func(int, int) (int, error)) error {
	fds := []int{0, 0}
	if e := unix.Pipe2(fds, unix.O_CLOEXEC|unix.O_NONBLOCK); e != nil {
		return e
	}
	//noinspection GoUnhandledErrorResult
	defer unix.Close(fds[0])
	//noinspection GoUnhandledErrorResult
	defer unix.Close(fds[1])
	iov := []unix.Iovec{{Base: &p[0]}}
	iov[0].SetLen(len(p))
	var n int
	var e error
	for {
		n, e = unix.Vmsplice(fds[1], iov, unix.SPLICE_F_GIFT|unix.SPLICE_F_NONBLOCK)
		if //noinspection GoDirectComparisonOfErrors
		e != unix.EINTR {
			break
		}
	}
	if e != nil {
		return e
	}
	if n < len(p) {
		return fmt.Errorf("vmsplice: short page gift %d < %d", n, len(p))
	}
	return drainAll(want, func(left int) (int, error) { return drain(fds[0], left) })
}

func drainAll(want int, fn func(int) (int, error)) error {
	for want > 0 {
		n, e := fn(want)
		if n < 0 || n > want {
			return errors.New("invalid splice count")
		}
		want -= n
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	return nil
}

func queueEmpty(fd int) error {
	var info unix.TCPInfo
	size := uint32(unsafe.Sizeof(info))
	_, _, errno := unix.Syscall6(unix.SYS_GETSOCKOPT, uintptr(fd), unix.IPPROTO_TCP, unix.TCP_INFO, uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&size)), 0)
	if errno != 0 {
		return errno
	}
	if uintptr(size) < unsafe.Offsetof(info.Notsent_bytes)+4 {
		return fmt.Errorf("%w: TCP_INFO lacks notsent_bytes", ErrUnsupported)
	}
	if info.State != 1 { // TCP_ESTABLISHED (Linux UAPI)
		return syscall.ECONNRESET
	}
	if info.Notsent_bytes != 0 {
		return unix.EAGAIN
	}
	return nil
}

func selectFake(p []byte) []byte {
	for _, method := range []string{"GET ", "POST ", "HEAD ", "PUT ", "DELETE ", "OPTIONS ", "CONNECT ", "TRACE ", "PATCH "} {
		if bytes.HasPrefix(p, []byte(method)) {
			return []byte("GET / HTTP/1.1\r\nHost: www.wikipedia.org\r\n\r\n")
		}
	}
	b := make([]byte, 517)
	copy(b, tlsPrefix)
	return b
}

// tlsPrefix contains the bytes from the string literal used to initialize
// tls_data in hufrea/byedpi's packets.c. The C array has 517 bytes; its
// remaining bytes are zero-filled. The anonymous mmap provides the same
// zero-filled tail when the fake is shorter than the outgoing prefix.
const tlsPrefix = "\x16\x03\x01\x02\x00\x01\x00\x01\xfc\x03\x03\x03\x5f" +
	"\x6f\x2c\xed\x13\x22\xf8\xdc\xb2\xf2\x60\x48\x2d\x72" +
	"\x66\x6f\x57\xdd\x13\x9d\x1b\x37\xdc\xfa\x36\x2e\xba" +
	"\xf9\x92\x99\x3a\x20\xf9\xdf\x0c\x2e\x8a\x55\x89\x82" +
	"\x31\x63\x1a\xef\xa8\xbe\x08\x58\xa7\xa3\x5a\x18\xd3" +
	"\x96\x5f\x04\x5c\xb4\x62\xaf\x89\xd7\x0f\x8b\x00\x3e" +
	"\x13\x02\x13\x03\x13\x01\xc0\x2c\xc0\x30\x00\x9f\xcc" +
	"\xa9\xcc\xa8\xcc\xaa\xc0\x2b\xc0\x2f\x00\x9e\xc0\x24" +
	"\xc0\x28\x00\x6b\xc0\x23\xc0\x27\x00\x67\xc0\x0a\xc0" +
	"\x14\x00\x39\xc0\x09\xc0\x13\x00\x33\x00\x9d\x00\x9c" +
	"\x00\x3d\x00\x3c\x00\x35\x00\x2f\x00\xff\x01\x00\x01" +
	"\x75\x00\x00\x00\x16\x00\x14\x00\x00\x11\x77\x77\x77" +
	"\x2e\x77\x69\x6b\x69\x70\x65\x64\x69\x61\x2e\x6f\x72" +
	"\x67\x00\x0b\x00\x04\x03\x00\x01\x02\x00\x0a\x00\x16" +
	"\x00\x14\x00\x1d\x00\x17\x00\x1e\x00\x19\x00\x18\x01" +
	"\x00\x01\x01\x01\x02\x01\x03\x01\x04\x00\x10\x00\x0e" +
	"\x00\x0c\x02\x68\x32\x08\x68\x74\x74\x70\x2f\x31\x2e" +
	"\x31\x00\x16\x00\x00\x00\x17\x00\x00\x00\x31\x00\x00" +
	"\x00\x0d\x00\x2a\x00\x28\x04\x03\x05\x03\x06\x03\x08" +
	"\x07\x08\x08\x08\x09\x08\x0a\x08\x0b\x08\x04\x08\x05" +
	"\x08\x06\x04\x01\x05\x01\x06\x01\x03\x03\x03\x01\x03" +
	"\x02\x04\x02\x05\x02\x06\x02\x00\x2b\x00\x09\x08\x03" +
	"\x04\x03\x03\x03\x02\x03\x01\x00\x2d\x00\x02\x01\x01" +
	"\x00\x33\x00\x26\x00\x24\x00\x1d\x00\x20\x11\x8c\xb8" +
	"\x8c\xe8\x8a\x08\x90\x1e\xee\x19\xd9\xdd\xe8\xd4\x06" +
	"\xb1\xd1\xe2\xab\xe0\x16\x63\xd6\xdc\xda\x84\xa4\xb8" +
	"\x4b\xfb\x0e\x00\x15\x00\xac\x00\x00\x00\x00\x00\x00"
