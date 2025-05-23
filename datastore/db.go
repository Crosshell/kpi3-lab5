package datastore

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	dataFileName    = "current-data"
	bufferSize      = 8192
	defaultFileMode = 0644
	minSegments     = 3
)

type keyIndex map[string]int64

type IndexOperation struct {
	isWrite  bool
	key      string
	position int64
}

type WriteOperation struct {
	data     entry
	response chan error
}

type KeyLocation struct {
	segment  *Segment
	position int64
}

type Db struct {
	activeFile      *os.File
	activeFilePath  string
	currentOffset   int64
	directory       string
	maxSegmentSize  int64
	segmentCounter  int
	indexOperations chan IndexOperation
	keyLocations    chan *KeyLocation
	writeOperations chan WriteOperation
	writeComplete   chan error
	keyIndex        keyIndex
	segments        []*Segment
	fileLock        sync.Mutex
	indexLock       sync.Mutex
}

type Segment struct {
	startOffset int64
	keyIndex    keyIndex
	path        string
}

func createDb(directory string, maxSegmentSize int64) (*Db, error) {
	database := &Db{
		segments:        make([]*Segment, 0),
		directory:       directory,
		maxSegmentSize:  maxSegmentSize,
		indexOperations: make(chan IndexOperation),
		keyLocations:    make(chan *KeyLocation),
		writeOperations: make(chan WriteOperation),
		writeComplete:   make(chan error),
	}

	if err := database.initializeNewSegment(); err != nil {
		return nil, err
	}

	if err := database.recoverAllSegments(); err != nil && err != io.EOF {
		return nil, err
	}

	database.startIndexHandler()
	database.startWriteHandler()

	return database, nil
}

func (db *Db) Close() error {
	if db.activeFile != nil {
		return db.activeFile.Close()
	}
	return nil
}

func (db *Db) startIndexHandler() {
	go func() {
		for operation := range db.indexOperations {
			db.indexLock.Lock()
			if operation.isWrite {
				db.updateIndex(operation.key, operation.position)
			} else {
				segment, pos, err := db.findKeyLocation(operation.key)
				if err != nil {
					db.keyLocations <- nil
				} else {
					db.keyLocations <- &KeyLocation{segment, pos}
				}
			}
			db.indexLock.Unlock()
		}
	}()
}

func (db *Db) startWriteHandler() {
	go func() {
		for operation := range db.writeOperations {
			db.fileLock.Lock()
			entrySize := operation.data.GetLength()

			fileInfo, err := db.activeFile.Stat()
			if err != nil {
				operation.response <- err
				db.fileLock.Unlock()
				continue
			}

			if fileInfo.Size()+entrySize > db.maxSegmentSize {
				if err := db.initializeNewSegment(); err != nil {
					operation.response <- err
					db.fileLock.Unlock()
					continue
				}
			}

			bytesWritten, err := db.activeFile.Write(operation.data.Encode())
			if err == nil {
				db.indexOperations <- IndexOperation{
					isWrite:  true,
					key:      operation.data.key,
					position: int64(bytesWritten),
				}
			}
			operation.response <- err
			db.fileLock.Unlock()
		}
	}()
}

func (db *Db) initializeNewSegment() error {
	newFilePath := db.generateFileName()
	file, err := os.OpenFile(newFilePath, os.O_APPEND|os.O_RDWR|os.O_CREATE, defaultFileMode)
	if err != nil {
		return err
	}

	segment := &Segment{
		path:     newFilePath,
		keyIndex: make(keyIndex),
	}

	db.activeFile = file
	db.currentOffset = 0
	db.activeFilePath = newFilePath
	db.segments = append(db.segments, segment)

	if len(db.segments) >= minSegments {
		db.compactOldSegments()
	}

	return nil
}

func (db *Db) generateFileName() string {
	fileName := filepath.Join(db.directory, fmt.Sprintf("%s%d", dataFileName, db.segmentCounter))
	db.segmentCounter++
	return fileName
}

func (db *Db) compactOldSegments() {
	go func() {
		compactedFilePath := db.generateFileName()
		compactedSegment := &Segment{
			path:     compactedFilePath,
			keyIndex: make(keyIndex),
		}

		var writeOffset int64
		compactedFile, err := os.OpenFile(compactedFilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, defaultFileMode)
		if err != nil {
			return
		}
		defer compactedFile.Close()

		lastIndex := len(db.segments) - 2
		for i := 0; i <= lastIndex; i++ {
			currentSegment := db.segments[i]
			for key, position := range currentSegment.keyIndex {
				if i < lastIndex {
					if db.keyExistsInNewerSegments(db.segments[i+1:lastIndex+1], key) {
						continue
					}
				}

				value, _ := currentSegment.readFromSegment(position)
				record := entry{
					key:   key,
					value: value,
				}

				bytesWritten, err := compactedFile.Write(record.Encode())
				if err == nil {
					compactedSegment.keyIndex[key] = writeOffset
					writeOffset += int64(bytesWritten)
				}
			}
		}
		db.segments = []*Segment{compactedSegment, db.getCurrentSegment()}
	}()
}

func (db *Db) keyExistsInNewerSegments(segments []*Segment, key string) bool {
	for _, segment := range segments {
		if _, exists := segment.keyIndex[key]; exists {
			return true
		}
	}
	return false
}

func (db *Db) recoverAllSegments() error {
	for _, segment := range db.segments {
		if err := db.recoverSegmentData(segment); err != nil {
			return err
		}
	}
	return nil
}

func (db *Db) recoverSegmentData(segment *Segment) error {
	file, err := os.Open(segment.path)
	if err != nil {
		return err
	}
	defer file.Close()

	return db.processRecovery(file)
}

func (db *Db) processRecovery(file *os.File) error {
	var err error
	var buffer [bufferSize]byte

	reader := bufio.NewReaderSize(file, bufferSize)
	for err == nil {
		var header, data []byte
		var bytesRead int

		header, err = reader.Peek(bufferSize)
		if err == io.EOF {
			if len(header) == 0 {
				return err
			}
		} else if err != nil {
			return err
		}

		recordSize := binary.LittleEndian.Uint32(header)

		if recordSize < bufferSize {
			data = buffer[:recordSize]
		} else {
			data = make([]byte, recordSize)
		}

		bytesRead, err = reader.Read(data)
		if err == nil {
			if bytesRead != int(recordSize) {
				return fmt.Errorf("data corruption detected")
			}

			var record entry
			record.Decode(data)
			db.updateIndex(record.key, int64(bytesRead))
		}
	}
	return err
}

func (db *Db) updateIndex(key string, bytesWritten int64) {
	db.getCurrentSegment().keyIndex[key] = db.currentOffset
	db.currentOffset += bytesWritten
}

func (db *Db) findKeyLocation(key string) (*Segment, int64, error) {
	for i := len(db.segments) - 1; i >= 0; i-- {
		segment := db.segments[i]
		if position, found := segment.keyIndex[key]; found {
			return segment, position, nil
		}
	}
	return nil, 0, fmt.Errorf("key not found in datastore")
}

func (db *Db) getKeyPosition(key string) *KeyLocation {
	operation := IndexOperation{
		isWrite: false,
		key:     key,
	}
	db.indexOperations <- operation
	return <-db.keyLocations
}

func (db *Db) Get(key string) (string, error) {
	location := db.getKeyPosition(key)
	if location == nil {
		return "", fmt.Errorf("key not found in datastore")
	}

	value, err := location.segment.readFromSegment(location.position)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (db *Db) Put(key, value string) error {
	responseChannel := make(chan error)
	db.writeOperations <- WriteOperation{
		data: entry{
			key:   key,
			value: value,
		},
		response: responseChannel,
	}

	err := <-responseChannel
	close(responseChannel)
	return err
}

func (db *Db) getCurrentSegment() *Segment {
	return db.segments[len(db.segments)-1]
}

func (segment *Segment) readFromSegment(position int64) (string, error) {
	file, err := os.Open(segment.path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = file.Seek(position, 0)
	if err != nil {
		return "", err
	}

	reader := bufio.NewReader(file)
	value, err := readValue(reader)
	if err != nil {
		return "", err
	}
	return value, nil
}
