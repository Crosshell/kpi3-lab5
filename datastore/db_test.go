package datastore

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const (
	testSegmentSize    = 45
	smallSegmentSize   = 35
	compactionWaitTime = 2 * time.Second
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

			time.Sleep(10 * time.Millisecond)

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

		time.Sleep(100 * time.Millisecond)

		currentFileInfo, err := firstSegmentFile.Stat()
		if err != nil {
			t.Fatal(err)
		}

		if initialSize != currentFileInfo.Size() {
			t.Errorf("File size changed unexpectedly: initial %d vs current %d", initialSize, currentFileInfo.Size())
		}
	})

	t.Run("database recovery after restart", func(t *testing.T) {
		time.Sleep(100 * time.Millisecond)
		database.Close()

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
		time.Sleep(50 * time.Millisecond)

		database.Put("2", "v2")
		time.Sleep(50 * time.Millisecond)

		database.Put("3", "v3")
		time.Sleep(50 * time.Millisecond)

		database.Put("2", "v5")
		time.Sleep(50 * time.Millisecond)

		time.Sleep(200 * time.Millisecond)

		finalSegmentCount := len(database.segments)
		if finalSegmentCount < 2 {
			t.Errorf("Expected at least 2 segments due to size limit, got %d", finalSegmentCount)
		}
	})

	t.Run("compaction trigger and completion", func(t *testing.T) {
		database.Put("4", "v4")
		database.Put("5", "v5")
		database.Put("6", "v6")

		time.Sleep(200 * time.Millisecond)

		segmentCountBeforeCompaction := len(database.segments)
		if segmentCountBeforeCompaction >= 3 {
			time.Sleep(compactionWaitTime)

			segmentCountAfterCompaction := len(database.segments)
			if segmentCountAfterCompaction >= segmentCountBeforeCompaction {
				t.Errorf("Compaction should reduce segment count: before %d, after %d",
					segmentCountBeforeCompaction, segmentCountAfterCompaction)
			}
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

	t.Run("compacted segment is not empty and valid", func(t *testing.T) {
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
		if actualSize == 0 {
			t.Errorf("Compacted segment file is empty, expected non-zero size")
		} else {
			t.Logf("Compacted segment file size: %d bytes", actualSize)
		}
	})
}

func TestDb_ParallelOperations(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "parallel_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	database, err := createTestDatabase(tempDir, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	t.Run("sequential put then parallel get", func(t *testing.T) {
		const numKeys = 50

		for i := 0; i < numKeys; i++ {
			key := fmt.Sprintf("key_%d", i)
			value := fmt.Sprintf("value_%d", i)

			err := database.Put(key, value)
			if err != nil {
				t.Errorf("Failed to put key %s: %v", key, err)
			}
		}

		time.Sleep(500 * time.Millisecond)

		var wg sync.WaitGroup
		errors := make(chan error, numKeys)

		for i := 0; i < numKeys; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				key := fmt.Sprintf("key_%d", index)
				expectedValue := fmt.Sprintf("value_%d", index)

				value, err := database.Get(key)
				if err != nil {
					errors <- fmt.Errorf("Failed to get key %s: %v", key, err)
					return
				}

				if value != expectedValue {
					errors <- fmt.Errorf("Value mismatch for key %s: expected %s, got %s", key, expectedValue, value)
					return
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Error(err)
		}
	})

	t.Run("parallel writes to different keys", func(t *testing.T) {
		const numWorkers = 5
		const keysPerWorker = 10

		var wg sync.WaitGroup
		errors := make(chan error, numWorkers*keysPerWorker)

		for workerID := 0; workerID < numWorkers; workerID++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < keysPerWorker; j++ {
					key := fmt.Sprintf("worker_%d_key_%d", id, j)
					value := fmt.Sprintf("worker_%d_value_%d", id, j)

					if err := database.Put(key, value); err != nil {
						errors <- fmt.Errorf("Worker %d failed to put key %s: %v", id, key, err)
						return
					}
				}
			}(workerID)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Error(err)
		}

		time.Sleep(1 * time.Second)

		for workerID := 0; workerID < numWorkers; workerID++ {
			for j := 0; j < keysPerWorker; j++ {
				key := fmt.Sprintf("worker_%d_key_%d", workerID, j)
				expectedValue := fmt.Sprintf("worker_%d_value_%d", workerID, j)

				value, err := database.Get(key)
				if err != nil {
					t.Errorf("Failed to get key %s: %v", key, err)
					continue
				}

				if value != expectedValue {
					t.Errorf("Value mismatch for key %s: expected %s, got %s", key, expectedValue, value)
				}
			}
		}
	})

	t.Run("concurrent writes to same key - final consistency", func(t *testing.T) {
		const numWorkers = 3
		const key = "shared_key"

		var wg sync.WaitGroup
		writtenValues := make([]string, numWorkers)

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				value := fmt.Sprintf("value_from_worker_%d", workerID)
				writtenValues[workerID] = value

				err := database.Put(key, value)
				if err != nil {
					t.Errorf("Worker %d failed to put: %v", workerID, err)
				}
			}(i)
		}

		wg.Wait()

		time.Sleep(1 * time.Second)

		finalValue, err := database.Get(key)
		if err != nil {
			t.Errorf("Failed to get final value: %v", err)
			return
		}

		found := false
		for _, expectedValue := range writtenValues {
			if finalValue == expectedValue {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Final value %s is not one of the expected values %v", finalValue, writtenValues)
		}
	})
}

func createTestDatabase(directory string, segmentSize int64) (*Db, error) {
	return createDb(directory, segmentSize)
}

func newDb(directory string, segmentSize int64) (*Db, error) {
	return createTestDatabase(directory, segmentSize)
}
