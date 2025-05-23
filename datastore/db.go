package datastore

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const outFileName = "current-data"

var ErrNotFound = fmt.Errorf("record does not exist")

type hashIndex map[string]int64

type Db struct {
	out       *os.File
	outOffset int64

	index hashIndex
}

func Open(dir string) (*Db, error) {
	outputPath := filepath.Join(dir, outFileName)
	f, err := os.OpenFile(outputPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	db := &Db{
		out:   f,
		index: make(hashIndex),
	}
	err = db.recover()
	if err != nil && err != io.EOF {
		return nil, err
	}
	return db, nil
}

func (db *Db) recover() error {
	f, err := os.Open(db.out.Name())
	if err != nil {
		return err
	}
	defer f.Close()

	in := bufio.NewReader(f)
	for err == nil {
		var (
			record entry
			n      int
		)
		n, err = record.DecodeFromReader(in)
		if errors.Is(err, io.EOF) {
			if n != 0 {
				return fmt.Errorf("corrupted file")
			}
			break
		}

		db.index[record.key] = db.outOffset
		db.outOffset += int64(n)
	}
	return err
}

func (db *Db) Close() error {
	return db.out.Close()
}

func (db *Db) Get(key string) (string, error) {
	position, ok := db.index[key]
	if !ok {
		return "", ErrNotFound
	}

	file, err := os.Open(db.out.Name())
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = file.Seek(position, 0)
	if err != nil {
		return "", err
	}

	var record entry
	if _, err = record.DecodeFromReader(bufio.NewReader(file)); err != nil {
		return "", err
	}
	return record.value, nil
}

func (db *Db) Put(key, value string) error {
	e := entry{
		key:   key,
		value: value,
	}
	n, err := db.out.Write(e.Encode())
	if err == nil {
		db.index[key] = db.outOffset
		db.outOffset += int64(n)
	}
	return err
}

func (db *Db) Size() (int64, error) {
	info, err := db.out.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
