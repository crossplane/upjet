package migration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/spf13/afero"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	sigsyaml "sigs.k8s.io/yaml"
)

var (
	_ Source = &FileSystemSource{}
	_ Target = &FileSystemTarget{}
)

// FileSystemSource is a source implementation to read resources from filesystem
type FileSystemSource struct {
	index int
	items []UnstructuredWithMetadata
	afero afero.Afero
}

// FileSystemSourceOption allows you to configure FileSystemSource
type FileSystemSourceOption func(*FileSystemSource)

// FsWithFileSystem configures the filesystem to use. Used mostly for testing.
func FsWithFileSystem(f afero.Fs) FileSystemSourceOption {
	return func(fs *FileSystemSource) {
		fs.afero = afero.Afero{Fs: f}
	}
}

// NewFileSystemSource returns a FileSystemSource
func NewFileSystemSource(dir string, opts ...FileSystemSourceOption) (*FileSystemSource, error) {
	fs := &FileSystemSource{
		afero: afero.Afero{Fs: afero.NewOsFs()},
	}
	for _, f := range opts {
		f(fs)
	}

	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("cannot read %s", path))
		}

		if info.IsDir() {
			return nil
		}

		data, err := fs.afero.ReadFile(path)
		if err != nil {
			return errors.Wrap(err, "cannot read source file")
		}

		decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(string(data)), 1024)
		u := &unstructured.Unstructured{}
		if err := decoder.Decode(&u); err != nil {
			return errors.Wrap(err, "cannot decode read data")
		}

		fs.items = append(fs.items, UnstructuredWithMetadata{
			Object: *u,
			Metadata: Metadata{
				Path:     path,
				Category: getCategory(*u),
			},
		})

		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "cannot read source directory")
	}

	return fs, nil
}

// HasNext checks the next item
func (fs *FileSystemSource) HasNext() (bool, error) {
	return fs.index < len(fs.items), nil
}

// Next returns the next item of slice
func (fs *FileSystemSource) Next() (UnstructuredWithMetadata, error) {
	if hasNext, _ := fs.HasNext(); hasNext {
		item := fs.items[fs.index]
		fs.index++
		return item, nil
	}
	return UnstructuredWithMetadata{}, errors.New("no more elements")
}

// Reset resets the source so that resources can be reread from the beginning.
func (fs *FileSystemSource) Reset() error {
	fs.index = 0
	return nil
}

// FileSystemTarget is a target implementation to write/patch/delete resources to file system
type FileSystemTarget struct {
	afero  afero.Afero
	parent string
}

// FileSystemTargetOption allows you to configure FileSystemTarget
type FileSystemTargetOption func(*FileSystemTarget)

// FtWithFileSystem configures the filesystem to use. Used mostly for testing.
func FtWithFileSystem(f afero.Fs) FileSystemTargetOption {
	return func(ft *FileSystemTarget) {
		ft.afero = afero.Afero{Fs: f}
	}
}

// WithParentDirectory configures the parent directory for the FileSystemTarget
func WithParentDirectory(parent string) FileSystemTargetOption {
	return func(ft *FileSystemTarget) {
		ft.parent = parent
	}
}

// NewFileSystemTarget returns a FileSystemTarget
func NewFileSystemTarget(opts ...FileSystemTargetOption) *FileSystemTarget {
	ft := &FileSystemTarget{
		afero: afero.Afero{Fs: afero.NewOsFs()},
	}
	for _, f := range opts {
		f(ft)
	}
	return ft
}

// Put writes input to filesystem
func (ft *FileSystemTarget) Put(o UnstructuredWithMetadata) error {
	b, err := sigsyaml.Marshal(o.Object.Object)
	if err != nil {
		return errors.Wrap(err, "cannot marshal object")
	}
	if err := os.MkdirAll(filepath.Join(ft.parent, filepath.Dir(o.Metadata.Path)), 0o750); err != nil {
		return errors.Wrapf(err, "cannot mkdirall: %s", filepath.Dir(o.Metadata.Path))
	}
	if o.Metadata.Parents != "" {
		f, err := ft.afero.OpenFile(filepath.Join(ft.parent, o.Metadata.Path), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			return errors.Wrap(err, "cannot open file")
		}

		defer f.Close() //nolint:errcheck

		if _, err = f.WriteString(fmt.Sprintf("\n---\n\n%s", string(b))); err != nil {
			return errors.Wrap(err, "cannot write file")
		}
	} else {
		f, err := ft.afero.Create(filepath.Join(ft.parent, o.Metadata.Path))
		if err != nil {
			return errors.Wrap(err, "cannot create file")
		}
		if _, err := f.Write(b); err != nil {
			return errors.Wrap(err, "cannot write file")
		}
	}

	return nil
}

// Delete deletes a file from filesystem
func (ft *FileSystemTarget) Delete(o UnstructuredWithMetadata) error {
	return ft.afero.Remove(o.Metadata.Path)
}
