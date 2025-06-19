package files

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Defines a file store. A file store gives an API to
// store files, abstracting the client from the real path and
// returning an URL to access the file.
type Store struct {
	url  string
	path string
}

// Creates a Store with a base url and a base path. For example,
// lets say we create a store with url http://localhost:8080/static and a base
// path of /home/user/static. We want to store the file /a/file.txt will be
// stored phiscally in /home/user/static/a/file.txt. The returned URL will
// be http://localhost:8080/static/a/file.txt.
func NewStore(url, path string) Store {
	return Store{url, path}
}

// Saves a byte buffer in a file located in a relative path. Returns a string with
// a URL to access the file. Can return an error if the file cannot be saved.
func (s Store) Save(buffer []byte, relativePath string) (string, error) {
	file, fullPath, err := s.createFile(relativePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	written, err := file.Write(buffer)
	if err != nil {
		return "", fmt.Errorf("error writing to file with path: '%s'. Error: %w", fullPath, err)
	}
	if written != len(buffer) {
		return "", fmt.Errorf("written to file %d bytes. Expected to write %d bytes", written, len(buffer))
	}

	return s.generateURL(relativePath), nil
}

func (s Store) generateURL(relativePath string) string {
	return fmt.Sprintf("%s/%s", s.url, relativePath)
}

// Copies a file in this store to other relative path in other store. For example,
// if you call a.Copy("/a/src.txt", "/b/target.txt", b) you are telling "Copy from
// file store 'a' the file stored in the relative path /a/src.txt to store 'b'
// in relative path /b/target.txt"
func (s Store) Copy(searchPath, targetPath string, other Store) (string, error) {
	outFile, err := s.openFile(searchPath)
	if err != nil {
		return "", err
	}
	defer outFile.Close()
	inFile, _, err := other.createFile(targetPath)
	if err != nil {
		return "", err
	}
	defer inFile.Close()
	reader := bufio.NewReader(outFile)
	writer := bufio.NewWriter(inFile)
	if _, err = reader.WriteTo(writer); err != nil {
		return "", err
	}
	return other.generateURL(targetPath), nil
}

// Check if a file exists in the store
func (s Store) Exists(relativePath string) bool {
	_, err := s.openFile(relativePath)
	return err == nil
}

func (s Store) openFile(relativePath string) (*os.File, error) {
	fullPath := fmt.Sprintf("%s/%s", s.path, relativePath)
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (s Store) createFile(relativePath string) (*os.File, string, error) {
	fullPath := fmt.Sprintf("%s/%s", s.path, relativePath)
	fileNameStart := strings.LastIndex(fullPath, "/")
	if err := os.MkdirAll(fullPath[:fileNameStart], os.ModePerm); err != nil {
		return nil, "", err
	}
	var file *os.File
	if _, err := os.Stat(fullPath); errors.Is(err, os.ErrNotExist) {
		if file, err = os.Create(fullPath); err != nil {
			return nil, fullPath, fmt.Errorf("cannot create new file with path: '%s'. Error: %w", fullPath, err)
		}
	} else {
		if file, err = os.OpenFile(fullPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.ModePerm); err != nil {
			return nil, fullPath, fmt.Errorf("cannot open file with path: '%s'. Error: %w", fullPath, err)
		}
	}
	return file, fullPath, nil
}

// DeleteOld files in the store. Files will be deleted if its modification
// time is older than the parameter 'duration'.
func (s Store) DeleteOld(duration time.Duration) error {
	return filepath.WalkDir(s.path, func(path string, file fs.DirEntry, err error) error {
		log.Println(path)
		if file.Type().IsRegular() {
			info, err := file.Info()
			if err != nil {
				return err
			}
			if time.Since(info.ModTime()) > duration {
				if err := os.Remove(path); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Opens a file using its relative path
func (s Store) Open(path string) (io.ReadWriteCloser, string, error) {
	return s.createFile(path)
}

// Delete a file using its relative path
func (s Store) Delete(relativePath string) error {
	fullPath := fmt.Sprintf("%s/%s", s.path, relativePath)
	return os.Remove(fullPath)
}
