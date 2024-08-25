package tls

import (
	"encoding/binary"
	"errors"
	"io"
)

func readClientHello(r io.Reader) ([]byte, error) {
	buf := make([]byte, 16*1024)

	// read at least 5 bytes
	n1, err := io.ReadAtLeast(r, buf, 5)
	if err != nil {
		return nil, err
	}

	_ = buf[4]

	if buf[0] != 0x16 {
		return nil, errors.New("tls: not a handshake")
	}

	n := int(5 + uint16(buf[4]) | uint16(buf[3])<<8)
	if n1 == n {
		return buf[:n1], nil
	}

	if n1 > n {
		return nil, errors.New("tls: too big handshake")
	}

	n2, err := io.ReadAtLeast(r, buf[n1:], n-n1)
	if err != nil {
		return nil, err
	}

	return buf[:n1+n2], nil
}

func parseSNI(hello []byte) string {
	// https://datatracker.ietf.org/doc/html/rfc8446#page-27
	// byte - content type (0x16 - handshake)
	// uint16 - version
	// uint16 - packet length

	// byte - message type (0x01 - client hello)
	// uint24 - message length
	// uint16 - version
	// [32]byte - random

	helloLen := uint16(len(hello))
	i := uint16(1 + 2 + 2 + 1 + 3 + 2 + 32) // session ID offset

	// byte - session ID length
	if i+1 > helloLen {
		return ""
	}
	sessionIDLen := uint16(hello[i])
	i += 1 + sessionIDLen // cipher suites offset

	// uint16 - cipher suites length
	if i+2 > helloLen {
		return ""
	}
	cipherSuitesLen := binary.BigEndian.Uint16(hello[i:])
	i += 2 + cipherSuitesLen // compression methods offset

	// byte - compression methods length
	if i+1 > helloLen {
		return ""
	}
	compressionMethodsLen := uint16(hello[i])
	i += 1 + compressionMethodsLen // extensions offset

	// uint16 - extensions length
	if i+2 > helloLen {
		return ""
	}
	extensionsLen := binary.BigEndian.Uint16(hello[i:])

	if i+2+extensionsLen > helloLen {
		return ""
	}
	return parseExtensions(hello[i+2 : i+2+extensionsLen])
}

func parseExtensions(data []byte) string {
	dataLen := uint16(len(data))

	for i := uint16(0); i < dataLen-4; {
		extType := binary.BigEndian.Uint16(data[i:])
		extLen := binary.BigEndian.Uint16(data[i+2:])
		i += 4

		if i+extLen > dataLen {
			break
		}

		const typeServerName = 0x00
		if extType == typeServerName {
			return parseSNIExtension(data[i : i+extLen])
		}

		i += extLen
	}

	return ""
}

func parseSNIExtension(data []byte) string {
	dataLen := uint16(len(data))

	if dataLen < 5 {
		return ""
	}

	listLen := binary.BigEndian.Uint16(data)
	if listLen != dataLen-2 {
		return ""
	}

	nameType := data[2]
	const typeHostName = 0x00
	if nameType != typeHostName {
		return ""
	}

	nameLen := binary.BigEndian.Uint16(data[3:])
	if nameLen != dataLen-5 {
		return ""
	}

	return string(data[5 : 5+nameLen])
}
