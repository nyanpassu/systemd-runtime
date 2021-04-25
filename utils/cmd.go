package utils

type Cmd struct {
	CmdPath     string
	Args        []string
	WorkingPath string
	Env         []string
}
