package migration

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/spf13/afero"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	sigsyaml "sigs.k8s.io/yaml"
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

	files, err := fs.afero.ReadDir(dir)
	if err != nil {
		log.Fatal(err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		data, err := fs.afero.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			return nil, err
		}

		decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(string(data)), 1024)
		u := &unstructured.Unstructured{}
		if err := decoder.Decode(&u); err != nil {
			if err != nil {
				return nil, err
			}
		}

		fs.items = append(fs.items, UnstructuredWithMetadata{
			Object: *u,
			Metadata: Metadata{
				Path: filepath.Join(dir, file.Name()),
			},
		})
	}

	return fs, nil
}

// HasNext checks the next item
func (fs *FileSystemSource) HasNext() (bool, error) {
	if fs.index < len(fs.items) {
		return true, nil
	}
	return false, nil
}

// Next returns the next item of slice
func (fs *FileSystemSource) Next() (UnstructuredWithMetadata, error) {
	if hasNext, _ := fs.HasNext(); hasNext {
		item := fs.items[fs.index]
		fs.index++
		return item, nil
	}
	return UnstructuredWithMetadata{}, errors.New("failed to get next element")
}

// FileSystemTarget is a target implementation to write/patch/delete resources to file system
type FileSystemTarget struct {
	afero afero.Afero
}

// FileSystemTargetOption allows you to configure FileSystemTarget
type FileSystemTargetOption func(*FileSystemTarget)

// FtWithFileSystem configures the filesystem to use. Used mostly for testing.
func FtWithFileSystem(f afero.Fs) FileSystemTargetOption {
	return func(ft *FileSystemTarget) {
		ft.afero = afero.Afero{Fs: f}
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
		return err
	}
	if o.Metadata.Parents != "" {
		f, err := ft.afero.OpenFile(o.Metadata.Path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			return err
		}

		defer f.Close() //nolint:errcheck

		if _, err = f.WriteString(fmt.Sprintf("\n---\n\n%s", string(b))); err != nil {
			return err
		}
	} else {
		f, err := ft.afero.Create(o.Metadata.Path)
		if err != nil {
			return err
		}
		if _, err := f.Write(b); err != nil {
			return err
		}
	}

	return nil
}

// Patch patches an existing file in filesystem
func (ft *FileSystemTarget) Patch(o UnstructuredWithMetadata) error {
	// no-op
	return nil
}

// Delete deletes a file from filesystem
func (ft *FileSystemTarget) Delete(o UnstructuredWithMetadata) error {
	return ft.afero.Remove(o.Metadata.Path)
}
