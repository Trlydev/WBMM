package scan

import (
	"encoding/binary"
)

// PE section layout, used to find the .NET metadata in a DLL.

type peLayout struct {
	opt, dirs           int
	fileAlign, secAlign uint32
	secs                []peSectionHdr
}

type peSectionHdr struct {
	hdr                     int // where its header is in the file
	va, vsize, raw, rawSize uint32
}

func peLayoutOf(b []byte) (*peLayout, error) {
	le := binary.LittleEndian
	if len(b) < 0x40 || b[0] != 'M' || b[1] != 'Z' {
		return nil, errNotCLR
	}
	pe := int(le.Uint32(b[0x3c:]))
	if pe+24 > len(b) || string(b[pe:pe+4]) != "PE\x00\x00" {
		return nil, errNotCLR
	}
	l := &peLayout{opt: pe + 24}
	nsec := int(le.Uint16(b[pe+6:]))
	optSize := int(le.Uint16(b[pe+20:]))
	if l.opt+optSize > len(b) || nsec == 0 {
		return nil, errNotCLR
	}
	switch le.Uint16(b[l.opt:]) {
	case 0x10b:
		l.dirs = l.opt + 96
	case 0x20b:
		l.dirs = l.opt + 112
	default:
		return nil, errNotCLR
	}
	l.secAlign, l.fileAlign = le.Uint32(b[l.opt+32:]), le.Uint32(b[l.opt+36:])
	if l.fileAlign == 0 || l.secAlign == 0 {
		return nil, errNotCLR
	}
	for i, at := 0, l.opt+optSize; i < nsec; i, at = i+1, at+40 {
		if at+40 > len(b) {
			return nil, errNotCLR
		}
		l.secs = append(l.secs, peSectionHdr{at, le.Uint32(b[at+12:]), le.Uint32(b[at+8:]), le.Uint32(b[at+20:]), le.Uint32(b[at+16:])})
	}
	return l, nil
}

func alignUp(v, a uint32) uint32 { return (v + a - 1) / a * a }

func maxU(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}
