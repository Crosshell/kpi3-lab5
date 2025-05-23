package datastore

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	testSegmentSize     = 45
	smallSegmentSize    = 35
	compactionWaitTime  = 2 * time.Second
	expectedSegmentSize = 45
)

func TestDb_Put(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "datastore_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	database, err := createTestDatabase(tempDir, testSegmentSize)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	testPairs := []struct {
		key   string
		value string
	}{
		{"1", "v1"},
		{"2", "v2"},
		{"3", "v3"},
	}

	firstSegmentFile, err := os.Open(filepath.Join(tempDir, dataFileName+"0"))
	if err != nil {
		t.Fatal(err)
	}
	defer firstSegmentFile.Close()

	t.Run("put and get operations", func(t *testing.T) {
		for _, pair := range testPairs {
			err := database.Put(pair.key, pair.value)
			if err != nil {
				t.Errorf("Failed to put key %s: %v", pair.key, err)
			}

			retrievedValue, err := database.Get(pair.key)
			if err != nil {
				t.Errorf("Failed to get key %s: %v", pair.key, err)
			}

			if retrievedValue != pair.value {
				t.Errorf("Value mismatch for key %s: expected %s, got %s", pair.key, pair.value, retrievedValue)
			}
		}
	})

	initialFileInfo, err := firstSegmentFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	initialSize := initialFileInfo.Size()

	t.Run("file size consistency on duplicate keys", func(t *testing.T) {
		for _, pair := range testPairs {
			err := database.Put(pair.key, pair.value)
			if err != nil {
				t.Errorf("Failed to put duplicate key %s: %v", pair.key, err)
			}
		}

		currentFileInfo, err := firstSegmentFile.Stat()
		if err != nil {
			t.Fatal(err)
		}

		if initialSize != currentFileInfo.Size() {
			t.Errorf("File size changed unexpectedly: initial %d vs current %d", initialSize, currentFileInfo.Size())
		}
	})

	t.Run("database recovery after restart", func(t *testing.T) {
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}

		recoveredDb, err := createTestDatabase(tempDir, 10)
		if err != nil {
			t.Fatal(err)
		}
		defer recoveredDb.Close()

		for _, pair := range testPairs {
			retrievedValue, err := recoveredDb.Get(pair.key)
			if err != nil {
				t.Errorf("Failed to get key %s after recovery: %v", pair.key, err)
			}

			if retrievedValue != pair.value {
				t.Errorf("Value mismatch after recovery for key %s: expected %s, got %s", pair.key, pair.value, retrievedValue)
			}
		}
	})
}

func TestDb_Segmentation(t *testing.T) {
	testDirectory, err := ioutil.TempDir("", "segmentation_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(testDirectory)

	database, err := createTestDatabase(testDirectory, smallSegmentSize)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	t.Run("segment creation on size limit", func(t *testing.T) {
		database.Put("1", "v1")
		database.Put("2", "v2")
		database.Put("3", "v3")
		database.Put("2", "v5")

		actualSegmentCount := len(database.segments)
		expectedSegmentCount := 2

		if actualSegmentCount != expectedSegmentCount {
			t.Errorf("Incorrect segment count after writes: expected %d, got %d", expectedSegmentCount, actualSegmentCount)
		}
	})

	t.Run("compaction trigger and completion", func(t *testing.T) {
		database.Put("4", "v4")

		segmentCountBeforeCompaction := len(database.segments)
		expectedBeforeCompaction := 3

		if segmentCountBeforeCompaction != expectedBeforeCompaction {
			t.Errorf("Incorrect segment count before compaction: expected %d, got %d", expectedBeforeCompaction, segmentCountBeforeCompaction)
		}

		time.Sleep(compactionWaitTime)

		segmentCountAfterCompaction := len(database.segments)
		expectedAfterCompaction := 2

		if segmentCountAfterCompaction != expectedAfterCompaction {
			t.Errorf("Incorrect segment count after compaction: expected %d, got %d", expectedAfterCompaction, segmentCountAfterCompaction)
		}
	})

	t.Run("data integrity after compaction", func(t *testing.T) {
		retrievedValue, err := database.Get("2")
		if err != nil {
			t.Errorf("Failed to retrieve key after compaction: %v", err)
		}

		expectedValue := "v5"
		if retrievedValue != expectedValue {
			t.Errorf("Data corruption after compaction: expected %s, got %s", expectedValue, retrievedValue)
		}
	})

	t.Run("compacted segment size validation", func(t *testing.T) {
		compactedSegmentFile, err := os.Open(database.segments[0].path)
		if err != nil {
			t.Error(err)
			return
		}
		defer compactedSegmentFile.Close()

		fileInfo, err := compactedSegmentFile.Stat()
		if err != nil {
			t.Error(err)
			return
		}

		actualSize := fileInfo.Size()
		expectedSize := int64(expectedSegmentSize)

		if actualSize != expectedSize {
			t.Errorf("Compacted segment size mismatch: expected %d, got %d", expectedSize, actualSize)
		}
	})
}

func createTestDatabase(directory string, segmentSize int64) (*Db, error) {
	return createDb(directory, segmentSize)
}

func newDb(directory string, segmentSize int64) (*Db, error) {
	return createTestDatabase(directory, segmentSize)
}
