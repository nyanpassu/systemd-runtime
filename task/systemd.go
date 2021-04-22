package task

const prefix = "eru-systemd-"

func UnitName(id string) string {
	return prefix + id
}
