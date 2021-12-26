/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package tasklog

import (
	"os"
	"path/filepath"
	"syscall"
)

// IsFifo checks if a file is a (named pipe) fifo
// if the file does not exist then it returns false
func IsFifo(path string) (bool, error) {
	stat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if stat.Mode()&os.ModeNamedPipe == os.ModeNamedPipe {
		return true, nil
	}
	return false, nil
}

// CheckOrCreateFifo .
func CheckOrCreateFifo(path string) (bool, error) {
	for retry := 3; ; retry-- {
		ok, err := isFifo(path)
		if err == nil || !os.IsNotExist(err) {
			return ok, err
		}
		if retry <= 0 {
			return false, nil
		}
		// file is not exist, we will try create fifo file
		if err = mkFifo(path); err == nil {
			return true, nil
		}
		if !os.IsExist(err) {
			// mk fifo error, return
			return false, err
		}
	}
}

func mkFifo(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), os.ModePerm); err != nil {
		return err
	}
	return syscall.Mkfifo(p, uint32(os.ModePerm))
}

func isFifo(path string) (bool, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if stat.Mode()&os.ModeNamedPipe == os.ModeNamedPipe {
		return true, nil
	}
	return false, nil
}
