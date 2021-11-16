package utils

import (
	"encoding/json"
	"io"
	"os"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// FileExists .
func FileExists(path string) (bool, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// EnsureDirExists .
func EnsureDirExists(path string) error {
	exists, err := FileExists(path)
	if err != nil {
		return err
	}
	if !exists {
		return os.Mkdir(path, 0644)
	}
	return nil
}

func FileReadJSON(file *os.File, dst interface{}) (bool, error) {
	content, err := FileResetRead(file)
	if err != nil {
		return false, err
	}
	if string(content) == "" {
		return false, nil
	}
	if err := json.Unmarshal(content, dst); err != nil {
		return false, err
	}
	return true, nil
}

func FileResetRead(file *os.File) (content []byte, err error) {
	if _, err := file.Seek(0, 0); err != nil {
		return nil, err
	}
	var size int
	if info, err := file.Stat(); err == nil {
		size64 := info.Size()
		if int64(int(size64)) == size64 {
			size = int(size64)
		}
	}
	size++

	if size < 512 {
		size = 512
	}

	data := make([]byte, 0, size)
	for {
		if len(data) >= cap(data) {
			d := append(data[:cap(data)], 0) //nolint:gocritic
			data = d[:len(data)]
		}
		n, err := file.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return data, err
		}
	}
}

func FileTruncateWrite(file *os.File, val []byte) error {
	offset, err := file.Seek(0, 0)
	if err != nil {
		return err
	}
	if offset != 0 {
		return errors.New("can not seek start of file")
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	_, err = file.Write(val)
	return err
}

func FileWriteJSON(file *os.File, val interface{}) error {
	content, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return FileTruncateWrite(file, content)
}

func FileClose(file *os.File, logger *logrus.Entry) {
	if err := file.Close(); err != nil {
		logger.WithError(err).WithField("FileName", file.Name()).Error("close file error")
	}
}
