package systemd

// import (
// 	"fmt"
// 	"os"
// 	"strconv"

// 	"github.com/projecteru2/systemd-runtime/oci/common"
// 	"github.com/nyanpassu/systemd-oci/utils"
// )

// // WritePid .
// func WritePid(id string, pid int) error {
// 	if err := utils.EnsureDirExists(common.ConfigDirPath); err != nil {
// 		return err
// 	}

// 	f, err := os.OpenFile(pidFile(id), os.O_RDWR|os.O_CREATE|os.O_SYNC, 0644)
// 	if err != nil {
// 		return err
// 	}
// 	_, err = f.WriteString(strconv.Itoa(pid))
// 	closeErr := f.Close()
// 	if err != nil {
// 		return err
// 	}
// 	return closeErr
// }

// // ReadPid .
// func ReadPid(id string) (int, bool, error) {
// 	content, err := os.ReadFile(pidFile(id))
// 	if os.IsNotExist(err) {
// 		return 0, false, nil
// 	}
// 	if err != nil {
// 		return 0, false, err
// 	}
// 	pid, err := strconv.Atoi(string(content))
// 	return pid, true, err
// }

// func pidFile(id string) string {
// 	return fmt.Sprintf("%s/%s/pid", common.ConfigDirPath, id)
// }
