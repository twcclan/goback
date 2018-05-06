package pack

import (
	"io/ioutil"
	"os"
	"testing"
)

var (
	temporaryDirectories []string
)

func getTempDir(t testingInterface) string {
	dir, err := ioutil.TempDir("", "goback")

	if err != nil {
		t.Fatalf("Couldn't create temporary directory for testing: %s", err)
	}

	t.Logf("Creating temporary dir %s", dir)

	temporaryDirectories = append(temporaryDirectories, dir)
	return dir
}

func TestMain(m *testing.M) {
	code := m.Run()

	if code == 0 {
		// clean up temporary folders, best effort
		for _, dir := range temporaryDirectories {
			os.RemoveAll(dir)
		}
	}

	os.Exit(code)
}
