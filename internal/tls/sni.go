package tls

import (
	"encoding/binary"

)

func parseSNI(hello []byte) (sni string) {
	if len(hello) < 43 {
		return ""
	}

	sessionIDLen := int(hello[43])
	cipherSuitesOffset := 44 + sessionIDLen
	if len(hello) < cipherSuitesOffset+2 {
		return ""
	}

	cipherSuitesLen := int(binary.BigEndian.Uint16(hello[cipherSuitesOffset:]))
	compressionMethodsOffset := cipherSuitesOffset + 2 + cipherSuitesLen
	if len(hello) < compressionMethodsOffset+1 {
		return ""
	}

	compressionMethodsLen := int(hello[compressionMethodsOffset])
	extensionsOffset := compressionMethodsOffset + 1 + compressionMethodsLen
	if len(hello) < extensionsOffset+2 {
		return ""
	}

	extensionsLen := int(binary.BigEndian.Uint16(hello[extensionsOffset:]))
	extensionsDataOffset := extensionsOffset + 2
	if len(hello) < extensionsDataOffset+extensionsLen {
		return ""
	}

	for i := extensionsDataOffset; i < extensionsDataOffset+extensionsLen; {
		if len(hello) < i+4 {
			return ""
		}
		extType := binary.BigEndian.Uint16(hello[i:])
		extLen := binary.BigEndian.Uint16(hello[i+2:])
		i += 4

		if extType == 0x00 { // SNI extension type
			if len(hello) < i+int(extLen) {
				return ""
			}
			sni = parseSNIExtension(hello[i : i+int(extLen)])
			break
		}
		i += int(extLen)
	}

	return sni
}

func parseSNIExtension(data []byte) string {
	if len(data) < 5 {
		return ""
	}
	listLen := binary.BigEndian.Uint16(data[0:])
	if listLen != uint16(len(data)-2) {
		return ""
	}
	nameType := data[2]
	if nameType != 0 {
		return ""
	}
	nameLen := binary.BigEndian.Uint16(data[3:])
	if int(nameLen) != len(data)-5 {
		return ""
	}
	return string(data[5 : 5+nameLen])
}
