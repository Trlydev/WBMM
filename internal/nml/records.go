package nml

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// RecordsFile is where NML keeps, per mod UID, its compile cache and
// whether the mod is turned off (StreamingAssets/mods/NML).
const RecordsFile = "mod_compile_records.json"

// legacyDisabledFile is the old list of turned-off mods, one UID per line.
// NML folds it into the records on its next start and deletes it.
const legacyDisabledFile = "disabled_mods.txt"

// Disabled returns the UIDs NML will not load.
func Disabled(nmlDir string) (map[string]bool, error) {
	out := map[string]bool{}
	recs, err := readRecords(nmlDir)
	if err != nil {
		return out, err
	}
	for uid, raw := range recs {
		var r struct {
			Disabled bool `json:"disabled"`
		}
		if json.Unmarshal(raw, &r) == nil && r.Disabled {
			out[uid] = true
		}
	}
	if f, err := os.Open(filepath.Join(nmlDir, legacyDisabledFile)); err == nil {
		s := bufio.NewScanner(f)
		for s.Scan() {
			if uid := strings.TrimSpace(s.Text()); uid != "" {
				out[uid] = true
			}
		}
		f.Close()
	}
	return out, nil
}

// SetDisabled turns a mod off (or back on) for NML's next start. Every
// other field NML wrote is kept as it was. The game must be closed: NML
// writes this file from memory while it runs.
func SetDisabled(nmlDir, uid string, disabled bool) error {
	recs, err := readRecords(nmlDir)
	if err != nil {
		return err
	}
	// Fields stay raw JSON: NML's timestamps are 18-digit numbers that a
	// float64 would round.
	rec := map[string]json.RawMessage{}
	if raw, ok := recs[uid]; ok {
		if err := json.Unmarshal(raw, &rec); err != nil {
			rec = map[string]json.RawMessage{}
		}
	}
	if len(rec) == 0 {
		// A mod NML hasn't seen yet. Timestamp 0 makes NML compile it on
		// its first start, as it would any new mod.
		id, _ := json.Marshal(uid)
		rec = map[string]json.RawMessage{
			"dependencies":          json.RawMessage("[]"),
			"mod_id":                id,
			"optional_dependencies": json.RawMessage("[]"),
			"compile_environment":   json.RawMessage("null"),
			"timestamp":             json.RawMessage("0"),
			"compile_timestamp":     json.RawMessage("0"),
		}
	}
	rec["disabled"] = json.RawMessage("false")
	if disabled {
		rec["disabled"] = json.RawMessage("true")
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	recs[uid] = b
	if err := writeRecords(nmlDir, recs); err != nil {
		return err
	}
	if !disabled {
		return dropLegacy(nmlDir, uid)
	}
	return nil
}

func readRecords(nmlDir string) (map[string]json.RawMessage, error) {
	recs := map[string]json.RawMessage{}
	b, err := os.ReadFile(filepath.Join(nmlDir, RecordsFile))
	if os.IsNotExist(err) {
		return recs, nil
	}
	if err != nil {
		// Not "empty": rewriting it from nothing would switch every mod
		// back on, held ones included.
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return recs, nil
	}
	if err := json.Unmarshal(b, &recs); err != nil {
		// NML starts over with an empty cache when it can't read the
		// file; do the same rather than refusing forever.
		return map[string]json.RawMessage{}, nil
	}
	if recs == nil {
		recs = map[string]json.RawMessage{}
	}
	return recs, nil
}

func writeRecords(nmlDir string, recs map[string]json.RawMessage) error {
	if err := os.MkdirAll(nmlDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(nmlDir, RecordsFile), b)
}

// dropLegacy removes uid from disabled_mods.txt, which NML would otherwise
// apply over the records on its next start.
func dropLegacy(nmlDir, uid string) error {
	p := filepath.Join(nmlDir, legacyDisabledFile)
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var keep []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && l != uid {
			keep = append(keep, l)
		}
	}
	return writeFileAtomic(p, []byte(strings.Join(keep, "\n")))
}

func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".wbmm-tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
