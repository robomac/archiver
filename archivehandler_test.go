package archiver

import (
	_ "embed"
	"strings"
	"testing"
)

func TestGetArchiveInfo(t *testing.T) {
	// runPath, _ := os.Getwd()
	testdata := []struct {
		testname      string
		filename      string
		filetype      ArchiveType
		embeddedcount int
		targetfile    string
		targettext    string
	}{{"7zip", "testassets/sz_test.7z", ARCHIVE_7Z, 3, "random_text.txt", "vulputate"},
		{"zip", "testassets/test.zip", ARCHIVE_ZIP, 2, "dirhelp.txt", "subdirectories"},
		// tgz include metadata in pseudo-files for each file, so double the file count.
		{"gzip", "testassets/tgz_test.tgz", ARCHIVE_TGZ, 4, "random_text.txt", "vulputate"},
		{"word", "testassets/Test Doc.docx", ARCHIVE_ZIP, -1, "", "Jubjub"},
	}

	for _, test := range testdata {
		ar, err := GetArchiveInfo(test.filename)
		if err != nil {
			t.Errorf("GetArchiveInfo() %s error = %v", test.testname, err)
		}
		if ar.ArchiveType != test.filetype {
			t.Errorf("%s wrong type ", test.testname)
		}
		if test.embeddedcount > 0 && len(ar.Files()) != test.embeddedcount {
			t.Errorf("%s wrong  file set length", test.testname)
		}
		archiveFile := ar.File("figgle.foo")
		if archiveFile != nil {
			t.Errorf("%s found non-existing file\n", test.testname)
		}

		if len(test.targetfile) > 0 { // Find and verify that file
			archiveFile = ar.File(test.targetfile)
			if archiveFile == nil {
				t.Errorf("%s missing random file %s\n", test.testname, test.targetfile)
			}
			fileBytes, err := archiveFile.GetBytes()
			if err != nil {
				t.Errorf("GetArchiveInfo() %s getbytes = %v", test.testname, err)
			}

			if !strings.Contains(string(fileBytes), test.targettext) {
				t.Errorf("%s file missing string", test.testname)
			}
		} else { // Go through all files
			foundIt := false
			for _, f := range ar.Files() {
				data, err := f.GetBytes()
				if err != nil {
					t.Errorf("GetArchiveInfo() %s getbytes = %v", test.testname, err)
				}
				if !foundIt {
					foundIt = strings.Contains(string(data), test.targettext)
				}
			}
			if !foundIt {
				t.Errorf("%s file missing string", test.testname)
			}
		}
	}
}
