package queue

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const spoolExt = ".fb"

type Spool struct {
	Dir string
}

func NewSpool(dir string) (*Spool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create spool dir: %w", err)
	}
	return &Spool{Dir: dir}, nil
}

// Write persists a batch atomically as <unix_nanos>-<rand>.fb. The filename
// timestamp is used to drain oldest-first.
func (s *Spool) Write(payload []byte) (string, error) {
	var randSuffix [4]byte
	if _, err := rand.Read(randSuffix[:]); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%d-%s%s", time.Now().UnixNano(), hex.EncodeToString(randSuffix[:]), spoolExt)
	final := filepath.Join(s.Dir, name)
	tmp := final + ".tmp"

	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return final, nil
}

// List returns spooled file paths sorted oldest-first.
func (s *Spool) List() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != spoolExt {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = filepath.Join(s.Dir, n)
	}
	return paths, nil
}

func (s *Spool) Read(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (s *Spool) Delete(path string) error {
	return os.Remove(path)
}
