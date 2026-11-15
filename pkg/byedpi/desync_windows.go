//go:build windows

package byedpi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"

	"golang.org/x/sys/windows"
)

const (
	tlsRecordHeaderSize = 5
	tlsHandshakeHeader  = 9
	tlsClientHello      = 1
	tlsSNIExtension     = 0
	disorderSNIShift    = 3
)

var errMalformedClientHello = errors.New("byedpi: malformed TLS ClientHello")

func desyncFirst(c *net.TCPConn, p []byte) (int, error) {
	recordEnd := tlsRecordHeaderSize + int(binary.BigEndian.Uint16(p[3:tlsRecordHeaderSize]))
	offset, err := sniOffset(p[:recordEnd])
	if err != nil {
		return 0, err
	}
	offset += disorderSNIShift
	if offset <= 1 || offset >= len(p) {
		return 0, errMalformedClientHello
	}
	if err := sendWindowsStrategy(c, p, offset); err != nil {
		_ = c.Close()
		return 0, err
	}
	return len(p), nil
}

// sniOffset returns the first hostname byte in a TLS ClientHello record.
func sniOffset(record []byte) (int, error) {
	if len(record) < tlsHandshakeHeader || record[0] != 0x16 || record[5] != tlsClientHello {
		return 0, errMalformedClientHello
	}
	p := record[tlsHandshakeHeader:]
	if len(p) < 34 {
		return 0, errMalformedClientHello
	}
	p = p[34:]
	skipVector := func(lengthSize int) bool {
		if len(p) < lengthSize {
			return false
		}
		length := int(p[0])
		if lengthSize == 2 {
			length = int(binary.BigEndian.Uint16(p[:2]))
		}
		if length > len(p)-lengthSize {
			return false
		}
		p = p[lengthSize+length:]
		return true
	}
	if !skipVector(1) || !skipVector(2) || !skipVector(1) || len(p) < 2 {
		return 0, errMalformedClientHello
	}

	extLen := int(binary.BigEndian.Uint16(p[:2]))
	p = p[2:]
	if extLen > len(p) {
		return 0, errMalformedClientHello
	}
	base := len(record) - len(p)
	extensions := p[:extLen]
	for offset := 0; len(extensions) >= 4; {
		typ := binary.BigEndian.Uint16(extensions[:2])
		length := int(binary.BigEndian.Uint16(extensions[2:4]))
		extensions = extensions[4:]
		if length > len(extensions) {
			return 0, errMalformedClientHello
		}
		if typ == tlsSNIExtension {
			nameList := extensions[:length]
			if len(nameList) < 5 || nameList[2] != 0 {
				return 0, errMalformedClientHello
			}
			nameLength := int(binary.BigEndian.Uint16(nameList[3:5]))
			if nameLength == 0 || nameLength > len(nameList)-5 {
				return 0, errMalformedClientHello
			}
			return base + offset + 9, nil
		}
		offset += 4 + length
		extensions = extensions[length:]
	}
	return 0, errMalformedClientHello
}

func sendWindowsStrategy(c *net.TCPConn, p []byte, disorderOffset int) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	remote, ok := c.RemoteAddr().(*net.TCPAddr)
	if !ok || remote.IP == nil {
		return errors.New("byedpi: invalid TCP remote address")
	}
	level, option := windows.IPPROTO_IP, windows.IP_TTL
	if remote.IP.To4() == nil {
		level, option = windows.IPPROTO_IPV6, windows.IPV6_UNICAST_HOPS
	}
	var originalTTL int
	var optionErr error
	err = raw.Control(func(fd uintptr) {
		originalTTL, optionErr = windows.GetsockoptInt(windows.Handle(fd), level, option)
	})
	if err != nil {
		return err
	}
	if optionErr != nil {
		return fmt.Errorf("read socket hop limit: %w", optionErr)
	}
	setTTL := func(value int) error {
		var setErr error
		err := raw.Control(func(fd uintptr) {
			setErr = windows.SetsockoptInt(windows.Handle(fd), level, option, value)
		})
		return errors.Join(err, setErr)
	}

	if err = writeAll(c, p[:1]); err != nil {
		return err
	}
	if err = setTTL(1); err != nil {
		return err
	}
	writeErr := writeAll(c, p[1:disorderOffset])
	restoreErr := setTTL(originalTTL)
	if err = errors.Join(writeErr, restoreErr); err != nil {
		return err
	}
	return writeAll(c, p[disorderOffset:])
}

func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if n < 0 || n > len(p) {
			return fmt.Errorf("byedpi: invalid write count %d", n)
		}
		p = p[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
