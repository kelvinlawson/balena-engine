package layer // import "github.com/docker/docker/layer"

import (
	"fmt"
	"github.com/docker/docker/pkg/stringid"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/opencontainers/go-digest"
)

func randomLayerID(seed int64) ChainID {
	r := rand.New(rand.NewSource(seed))

	return ChainID(digest.FromBytes([]byte(fmt.Sprintf("%d", r.Int63()))))
}

func newFileMetadataStore(t *testing.T) (*fileMetadataStore, string, func()) {
	td, err := ioutil.TempDir("", "layers-")
	if err != nil {
		t.Fatal(err)
	}
	fms, err := newFSMetadataStore(td)
	if err != nil {
		t.Fatal(err)
	}

	return fms, td, func() {
		if err := os.RemoveAll(td); err != nil {
			t.Logf("Failed to cleanup %q: %s", td, err)
		}
	}
}

func assertNotDirectoryError(t *testing.T, err error) {
	perr, ok := err.(*os.PathError)
	if !ok {
		t.Fatalf("Unexpected error %#v, expected path error", err)
	}

	if perr.Err != syscall.ENOTDIR {
		t.Fatalf("Unexpected error %s, expected %s", perr.Err, syscall.ENOTDIR)
	}
}

func TestCommitFailure(t *testing.T) {
	fms, td, cleanup := newFileMetadataStore(t)
	defer cleanup()

	if err := ioutil.WriteFile(filepath.Join(td, "sha256"), []byte("was here first!"), 0644); err != nil {
		t.Fatal(err)
	}

	tx, err := fms.StartTransaction(stringid.GenerateRandomID())
	if err != nil {
		t.Fatal(err)
	}

	if err := tx.SetSize(0); err != nil {
		t.Fatal(err)
	}

	err = tx.Commit(randomLayerID(5))
	if err == nil {
		t.Fatalf("Expected error committing with invalid layer parent directory")
	}
	assertNotDirectoryError(t, err)
}

func TestStartTransactionFailure(t *testing.T) {
	fms, td, cleanup := newFileMetadataStore(t)
	defer cleanup()

	if err := ioutil.WriteFile(filepath.Join(td, "tmp"), []byte("was here first!"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := fms.StartTransaction(stringid.GenerateRandomID())
	if err == nil {
		t.Fatalf("Expected error starting transaction with invalid layer parent directory")
	}
	assertNotDirectoryError(t, err)

	if err := os.Remove(filepath.Join(td, "tmp")); err != nil {
		t.Fatal(err)
	}

	tx, err := fms.StartTransaction(stringid.GenerateRandomID())
	if err != nil {
		t.Fatal(err)
	}

	if expected := filepath.Join(td, "tmp"); strings.HasPrefix(expected, tx.String()) {
		t.Fatalf("Unexpected transaction string %q, expected prefix %q", tx.String(), expected)
	}

	if err := tx.Cancel(); err != nil {
		t.Fatal(err)
	}
}

func TestFileMetadataStore_StartTransaction(t *testing.T) {
	fms, _, cleanup := newFileMetadataStore(t)
	defer cleanup()

	tx, err := fms.StartTransaction("test-start-transaction")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Cancel()

	txData, err := fms.ListExistingTransactions()
	if err != nil {
		t.Fatal(err)
	}
	if len(txData) != 1 {
		t.Errorf("Expected single transaction")
	} else {
		cacheID, err := txData[0].GetCacheID()
		if err != nil {
			t.Fatal(err)
		}
		if cacheID != "test-start-transaction" {
			t.Errorf("Unexpected cache ID in the started transaction: %s", cacheID)
		}
	}
}

func TestFileMetadataStore_ListExistingTransactions(t *testing.T) {
	fms, _, cleanup := newFileMetadataStore(t)
	defer cleanup()

	t.Run("no tmp directory", func(t *testing.T) {
		list, err := fms.ListExistingTransactions()
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 0 {
			t.Errorf("Existing transactions returned when unexpected: %s", list)
		}
	})

	t.Run("new transactions case", func(t *testing.T) {
		tx1, err := fms.StartTransaction("1")
		if err != nil {
			t.Fatal(err)
		}
		defer tx1.Cancel()
		tx2, err := fms.StartTransaction("2")
		if err != nil {
			t.Fatal(err)
		}
		defer tx2.Cancel()
		list, err := fms.ListExistingTransactions()
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 {
			t.Errorf("Expected 2 existing transactions, but got %d", len(list))
		}

		tx1.Commit(randomLayerID(42))
		list, err = fms.ListExistingTransactions()
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 {
			t.Errorf("Expected 1 existing transaction after comitting another, but got %d", len(list))
		} else {
			if cacheID, err := list[0].GetCacheID(); err != nil {
				t.Fatal(err)
			} else if cacheID != "2" {
				t.Errorf("Wrong cache ID of the only existing transaction: %s", cacheID)
			}
		}

		tx2.Cancel()
		list, err = fms.ListExistingTransactions()
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 0 {
			t.Errorf("Expected none existing transactions, but got %d", len(list))
		}
	})

	t.Run("clean up case", func(t *testing.T) {
		tx1, err := fms.StartTransaction(stringid.GenerateRandomID())
		if err != nil {
			t.Fatal(err)
		}
		defer tx1.Cancel()

		list, err := fms.ListExistingTransactions()
		if err != nil {
			t.Fatal(err)
		}
		if len(list) == 1 {
			err = list[0].Delete()
			if err != nil {
				t.Fatal(err)
			}
			newlist, err := fms.ListExistingTransactions()
			if err != nil {
				t.Fatal(err)
			}
			if len(newlist) != 0 {
				t.Errorf("Unexpected transactions data: %s", newlist)
			}
		} else {
			t.Errorf("Expected only one transaction, but got %s", list)
		}
	})
}
