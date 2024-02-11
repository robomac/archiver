package archiver

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"time"

	"github.com/bodgit/sevenzip"
)

// Type of archive this is determined to be
type ArchiveType int

const (
	ARCHIVE_UNINIT = iota // Not yet determined
	ARCHIVE_NA            // Not an archive
	ARCHIVE_ZIP           // Zip
	ARCHIVE_TGZ
	ARCHIVE_7Z
)

type ArchiveInfo struct {
	path        string      // File path to archive file
	name        string      // Name of archive file
	fullname    string      // used internally.
	size        int64       // File size.
	ArchiveType ArchiveType // Type of archive (or na)
	files       []ArchivedFile
}

func (ai *ArchiveInfo) Size() int64           { return ai.size }
func (ai *ArchiveInfo) Name() string          { return ai.name }
func (ai *ArchiveInfo) Path() string          { return ai.path }
func (ai *ArchiveInfo) Files() []ArchivedFile { return ai.files }

// Return pointer to the named file.  Name must be exact.
func (ai *ArchiveInfo) File(fname string) *ArchivedFile {
	if len(ai.files) == 0 {
		return nil
	}
	idx := slices.IndexFunc(ai.files, func(e ArchivedFile) bool { return e.name == fname })
	if idx == -1 {
		return nil
	}
	return &ai.files[idx]
}

// Same as os.fileStat, implements/extends fs.FileInfo
type ArchivedFile struct {
	archivefile string      // Full path to the host archive
	archivetype ArchiveType // For use with GetBytes
	name        string      // Name of this file in the archive.  May include dir-sep
	size        int64
	IsDir       bool
	mode        fs.FileMode
	modTime     time.Time
}

func (fs *ArchivedFile) Path() string       { return fs.archivefile }
func (fs *ArchivedFile) Name() string       { return fs.name }
func (fs *ArchivedFile) Size() int64        { return fs.size }
func (fs *ArchivedFile) Mode() fs.FileMode  { return fs.mode }
func (fs *ArchivedFile) ModTime() time.Time { return fs.modTime }
func (fs *ArchivedFile) Sys() any           { return 0 }

func GetArchiveInfo(path string) (ar *ArchiveInfo, err error) {
	var arinstance ArchiveInfo
	ar = &arinstance
	if strings.Contains(path, string(os.PathSeparator)) { // Replace CWD with specified.
		pathName := filepath.Dir(path)
		if len(pathName) == 0 {
			fmt.Printf("Error: Invalid input path.\n")
			return
		}
		ar.path = pathName
		ar.name = filepath.Base(path)
	} else {
		ar.path, _ = os.Getwd()
		ar.name = path
	}
	// Verify there's a file there.
	ar.fullname = filepath.Join(ar.path, ar.name)
	fs, err := os.Stat(ar.fullname)
	if err == nil {
		ar.size = fs.Size()
		err = ar.getArchiveType()
	}
	if err == nil {
		switch ar.ArchiveType {
		case ARCHIVE_7Z:
			err = ar.loadFilesIn7ZArchive()
		case ARCHIVE_TGZ:
			err = ar.loadFilesInTgzArchive()
		case ARCHIVE_ZIP:
			err = ar.loadFilesInZipArchive()
		}
	}
	return ar, err
}

// This will reset ai.ArchiveType.  Determined type by magic header bytes, not extension
func (ar *ArchiveInfo) getArchiveType() error {
	if ar.size < 5 {
		ar.ArchiveType = ARCHIVE_NA
		return nil
	}
	filebytes := make([]byte, 5)
	file, err := os.Open(ar.fullname)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Read(filebytes)

	if err != nil {
		return err
	}
	switch {
	case (filebytes[0] == 0x50) && (filebytes[1] == 0x4B) && (filebytes[2] == 0x03) && (filebytes[3] == 0x04):
		ar.ArchiveType = ARCHIVE_ZIP
	case (filebytes[0] == 0x37) && (filebytes[1] == 0x7A) && (filebytes[2] == 0xBC) && (filebytes[3] == 0xAF):
		ar.ArchiveType = ARCHIVE_7Z
	case (filebytes[0] == 0x1F) && (filebytes[1] == 0x8B):
		ar.ArchiveType = ARCHIVE_TGZ
	default:
		ar.ArchiveType = ARCHIVE_NA
	}
	return nil
}

func (af *ArchivedFile) extractZipFileBytes() ([]byte, error) {
	var buffer = make([]byte, af.size)
	zipReader, err := zip.OpenReader(af.archivefile)
	if err != nil {
		err2 := fmt.Errorf("Could not open %s.  %w", af.archivefile, err) //lint:ignore ST1005 Casing is good
		return nil, err2
	}
	defer zipReader.Close()

	for _, fileInZip := range zipReader.File {
		if fileInZip.Name != af.name {
			continue
		}
		readCloser, err := fileInZip.Open()
		if err != nil {
			return nil, err
		}
		defer readCloser.Close()
		readCloser.Read(buffer)
		break
	}
	return buffer, err
}

func (af *ArchivedFile) extract7ZFileBytes() ([]byte, error) {
	zipReader, err := sevenzip.OpenReader(af.archivefile)
	if err != nil {
		err2 := fmt.Errorf("Could not open %s.  %w", af.archivefile, err) //lint:ignore ST1005 Casing is good
		return nil, err2
	}
	var buffer = make([]byte, af.size)

	for _, fileInZip := range zipReader.File {
		if fileInZip.Name != af.name {
			continue
		}
		readCloser, err := fileInZip.Open()
		if err != nil {
			return nil, err
		}
		defer readCloser.Close()
		readCloser.Read(buffer)
		break
	}
	return buffer, err
}

func (af *ArchivedFile) extractTgzFileBytes() ([]byte, error) {
	var gzReader *gzip.Reader
	var tarReader *tar.Reader
	var buffer = make([]byte, af.size)

	file, err := os.Open(af.archivefile)
	if err == nil {
		defer file.Close()
		gzReader, err = gzip.NewReader(file)
	}
	if err == nil {
		defer gzReader.Close()
		tarReader = tar.NewReader(gzReader)
	}
	if err != nil {
		err2 := fmt.Errorf("Could not open %s.  %w", af.archivefile, err) //lint:ignore ST1005 Casing is good
		return nil, err2
	}

	// Locate file
	head, err := tarReader.Next()
	for head != nil && err == nil {
		if head.Name != af.name {
			head, err = tarReader.Next()
			continue
		}
		break
	}
	// Pseudo-Seek done.  Uggah.  Read data
	tarReader.Read(buffer)
	return buffer, err
}

// To Do - Verify this gets directory-embedded files in the zip also
func (ar *ArchiveInfo) loadFilesInZipArchive() error {
	zipReader, err := zip.OpenReader(ar.fullname)
	if err != nil {
		err2 := fmt.Errorf("Could not open %s.  %w", ar.fullname, err) //lint:ignore ST1005 Casing is good
		return err2
	}
	defer zipReader.Close()

	for _, fileInZip := range zipReader.File {
		var arFile ArchivedFile = ArchivedFile{ar.fullname, ARCHIVE_ZIP, fileInZip.Name, int64(fileInZip.UncompressedSize64),
			fileInZip.FileInfo().IsDir(), fileInZip.Mode(), fileInZip.ModTime()}
		ar.files = append(ar.files, arFile)
	}
	return err
}

func (ar *ArchiveInfo) loadFilesIn7ZArchive() error {
	zipReader, err := sevenzip.OpenReader(ar.fullname)
	if err != nil {
		//lint:ignore ST1005 Casing is good
		err2 := fmt.Errorf("Could not open %s.  %w", ar.fullname, err)
		return err2
	}
	defer zipReader.Close()

	for _, fileInZip := range zipReader.File {
		var arFile ArchivedFile = ArchivedFile{ar.fullname, ARCHIVE_7Z, fileInZip.Name, int64(fileInZip.FileInfo().Size()),
			fileInZip.FileInfo().IsDir(), fileInZip.Mode(), fileInZip.Modified}
		ar.files = append(ar.files, arFile)
	}
	return err
}

func (ar *ArchiveInfo) loadFilesInTgzArchive() error {
	var gzReader *gzip.Reader
	var tarReader *tar.Reader

	file, err := os.Open(ar.fullname)
	if err == nil {
		defer file.Close()
		gzReader, err = gzip.NewReader(file)
	}
	if err == nil {
		defer gzReader.Close()
		tarReader = tar.NewReader(gzReader)
	}
	if err != nil {
		//lint:ignore ST1005 Casing is good
		err2 := fmt.Errorf("Could not open %s.  %w", ar.fullname, err)
		return err2
	}

	head, err := tarReader.Next()
	for head != nil && err == nil {
		var arFile ArchivedFile = ArchivedFile{ar.fullname, ARCHIVE_TGZ, head.Name, head.Size, false, head.FileInfo().Mode(), head.ModTime}
		ar.files = append(ar.files, arFile)

		head, err = tarReader.Next()
	}
	if err == io.EOF {
		return nil
	}
	return err
}

func (af *ArchivedFile) GetBytes() ([]byte, error) {
	switch af.archivetype {
	case ARCHIVE_7Z:
		return af.extract7ZFileBytes()
	case ARCHIVE_TGZ:
		return af.extractTgzFileBytes()
	case ARCHIVE_ZIP:
		return af.extractZipFileBytes()
	}
	return nil, errors.New("unsupported archive type")
}
