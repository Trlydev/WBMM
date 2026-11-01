// Package modpack reads and writes wbmm modpacks: a list of mods, small
// enough to paste into a chat, saved as a .wbm file.
//
// A modpack is one line of text: "WBM1." and then, base64url-encoded and
// deflate-compressed, a tiny binary list. Each entry says where the mod
// comes from (GameBanana or the Steam Workshop) and its ID there, which file
// (GameBanana), whether it's switched on, and its name (for showing the list
// before anything is downloaded). Mods installed by hand can't be fetched,
// so only their names travel, to say what's missing. The pack names no URLs
// and carries no code: wbmm installs from GameBanana and the Workshop as
// always, and every mod goes through the safety check.
package modpack

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Prefix starts every modpack code.
const Prefix = "WBM1."

// Ext is the modpack file extension.
const Ext = ".wbm"

// Limits that keep a pasted code from costing anything to read.
const (
	MaxEntries = 1000
	maxRaw     = 256 << 10
	maxName    = 48
)

// Source of an entry.
const (
	GameBanana = 0
	Workshop   = 1
	Local      = 2 // installed by hand: only the name travels
)

// Entry is one mod of a pack.
type Entry struct {
	Source  int
	ID      uint64 // GameBanana mod ID or Workshop item ID
	FileID  uint64 // GameBanana file ID; 0 means the newest
	Enabled bool
	Name    string
}

// Pack is a named list of mods.
type Pack struct {
	Name    string
	Entries []Entry
}

// Encode returns the pack as a modpack code.
func (p Pack) Encode() string {
	var raw bytes.Buffer
	putString(&raw, clip(p.Name, 80))
	putUvarint(&raw, uint64(len(p.Entries)))
	for _, e := range p.Entries {
		flags := byte(e.Source & 3)
		if !e.Enabled {
			flags |= 4
		}
		if e.FileID != 0 {
			flags |= 8
		}
		raw.WriteByte(flags)
		if e.Source != Local {
			putUvarint(&raw, e.ID)
		}
		if e.FileID != 0 {
			putUvarint(&raw, e.FileID)
		}
		putString(&raw, clip(e.Name, maxName))
	}
	var z bytes.Buffer
	w, _ := flate.NewWriter(&z, flate.BestCompression)
	w.Write(raw.Bytes())
	w.Close()
	return Prefix + base64.RawURLEncoding.EncodeToString(z.Bytes())
}

// peoplePlaygroundPrefix starts the codes of PPG Mod Manager (ppgmods),
// wbmm's sister app for People Playground; its files are .ppgm.
const peoplePlaygroundPrefix = "PPG1."

// ErrPeoplePlayground: the code is a People Playground modpack, for ppgmods.
var ErrPeoplePlayground = errors.New("this is a People Playground modpack (PPG1. code): open it with PPG Mod Manager, https://github.com/Trlydev/SWG")

// Decode reads a modpack code (or a .wbm file's text). Anything around the
// code (spaces, line breaks, a chat message) is ignored.
func Decode(text string) (Pack, error) {
	i := strings.Index(text, Prefix)
	if i < 0 && strings.Contains(text, peoplePlaygroundPrefix) {
		return Pack{}, ErrPeoplePlayground
	}
	if i < 0 {
		return Pack{}, errors.New("this isn't a wbmm modpack (no WBM1. code in it)")
	}
	code := text[i+len(Prefix):]
	if j := strings.IndexFunc(code, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}); j >= 0 {
		code = code[:j]
	}
	z, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		return Pack{}, errors.New("the modpack code is cut short or changed")
	}
	raw, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(z)), maxRaw+1))
	if err != nil || len(raw) > maxRaw {
		return Pack{}, errors.New("the modpack code is damaged")
	}
	r := bytes.NewReader(raw)
	var p Pack
	if p.Name, err = getString(r); err != nil {
		return Pack{}, err
	}
	n, err := binary.ReadUvarint(r)
	if err != nil || n > MaxEntries {
		return Pack{}, errors.New("the modpack code is damaged")
	}
	for k := uint64(0); k < n; k++ {
		flags, err := r.ReadByte()
		if err != nil {
			return Pack{}, errors.New("the modpack code is damaged")
		}
		e := Entry{Source: int(flags & 3), Enabled: flags&4 == 0}
		if e.Source > Local {
			return Pack{}, fmt.Errorf("the modpack names a source this wbmm doesn't know (made by a newer version?)")
		}
		if e.Source != Local {
			if e.ID, err = binary.ReadUvarint(r); err != nil || e.ID == 0 {
				return Pack{}, errors.New("the modpack code is damaged")
			}
		}
		if flags&8 != 0 {
			if e.FileID, err = binary.ReadUvarint(r); err != nil {
				return Pack{}, errors.New("the modpack code is damaged")
			}
		}
		if e.Name, err = getString(r); err != nil {
			return Pack{}, err
		}
		p.Entries = append(p.Entries, e)
	}
	return p, nil
}

func putUvarint(b *bytes.Buffer, v uint64) {
	var tmp [binary.MaxVarintLen64]byte
	b.Write(tmp[:binary.PutUvarint(tmp[:], v)])
}

func putString(b *bytes.Buffer, s string) {
	putUvarint(b, uint64(len(s)))
	b.WriteString(s)
}

func getString(r *bytes.Reader) (string, error) {
	n, err := binary.ReadUvarint(r)
	if err != nil || n > 1024 || int(n) > r.Len() {
		return "", errors.New("the modpack code is damaged")
	}
	b := make([]byte, n)
	r.Read(b)
	s := strings.ToValidUTF8(string(b), "")
	return strings.Map(func(c rune) rune {
		if c < 0x20 {
			return -1
		}
		return c
	}, s), nil
}

// clip shortens s to at most n bytes without splitting a character.
func clip(s string, n int) string {
	for len(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}
