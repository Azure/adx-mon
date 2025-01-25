package version

import "fmt"

var (
	GitCommit string
	BuildTime string
	Version   string
)

func String() string {
	return fmt.Sprintf("%s, Commit:%s, Build-time:%s", Version, GitCommit, BuildTime)
}
