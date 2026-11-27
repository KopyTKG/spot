package mod

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	maxEvery     = 300
	maxOneFile   = 4 << 20
	maxEveryByte = 16 << 20
)

// All is every file in the mod folder dir, by its path inside: what an approval
// covers. A .git folder is left out, so that a mod can be developed in one;
// nothing in it is read or run by spot.
func All(dir string) (map[string][]byte, error) {
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	total := 0
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if strings.ContainsFunc(rel, func(r rune) bool { return !unicode.IsPrint(r) }) {
			return fmt.Errorf("a file name with control or invisible characters: %q", rel)
		}
		if d.Type()&fs.ModeSymlink != 0 && rel != "." {
			return fmt.Errorf("%s: a symlink in a mod", rel)
		}
		if d.IsDir() {
			return nil
		}
		if st, err := os.Lstat(path); err != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("%s: not a plain file", rel)
		}
		if len(files) >= maxEvery {
			return fmt.Errorf("more than %d files", maxEvery)
		}
		b, err := readExact(path, maxOneFile)
		if err != nil {
			return err
		}
		if total += len(b); total > maxEveryByte {
			return fmt.Errorf("more than %d MB in all", maxEveryByte>>20)
		}
		files[rel] = b
		return nil
	})
	return files, err
}
