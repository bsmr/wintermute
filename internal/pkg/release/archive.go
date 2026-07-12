package release

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Untar extracts a gzipped tar stream into dst, preserving file modes.
func Untar(r io.Reader, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.Clean("/"+hdr.Name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return err
			}
			// #nosec G110 -- Untar consumes our own freshly-built systools:make_tar
			// output during `wm release --self-contained`, not untrusted input.
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			_ = os.Symlink(hdr.Linkname, target)
		}
	}
}

// TarGz writes a gzipped tar of srcDir's contents (paths relative to srcDir) to w,
// preserving file modes.
func TarGz(srcDir string, w io.Writer) (err error) {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	// Close tw then gz (LIFO would reverse it) and surface a flush/close failure
	// when the walk itself succeeded — a truncated archive must never be reported
	// as success (e.g. a full disk during the final flush).
	defer func() {
		if cerr := tw.Close(); err == nil {
			err = cerr
		}
		if cerr := gz.Close(); err == nil {
			err = cerr
		}
	}()
	return filepath.Walk(srcDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		link := ""
		if fi.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(fi, link)
		if err != nil {
			return err
		}
		hdr.Name = strings.ReplaceAll(rel, string(os.PathSeparator), "/")
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		// #nosec G122 -- TarGz walks a work dir wm itself just built and owns
		// exclusively (no concurrent mutation, no attacker-controlled symlinks).
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
}
