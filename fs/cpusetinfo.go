package fs

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"bazil.org/fuse"
	fusefs "bazil.org/fuse/fs"
	"github.com/Sirupsen/logrus"
	"golang.org/x/net/context"
)

type CpuInfoFile struct {
	cgroupdir string
}

func init() {
	fileMap["cpuinfo"] = FileInfo{
		initFunc:   NewCpuInfoFile,
		inode:      INODE_CPUINFO,
		subsysName: "cpuset",
	}
}

var (
	cpuinfo                 = make(map[int]string)
	pattern  string         = "processor\\s+?:\\s+?\\d"
	replacer *regexp.Regexp = nil
	buffer   bytes.Buffer
)

func NewCpuInfoFile(cgroupdir string) fusefs.Node {
	return CpuInfoFile{cgroupdir}
}

func (ci CpuInfoFile) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Inode = INODE_CPUINFO
	a.Mode = 0444
	data, _ := ci.ReadAll(ctx)
	a.Size = uint64(len(data))

	return nil
}

func (ci CpuInfoFile) ReadAll(ctx context.Context) ([]byte, error) {
	buffer.Reset()
	if replacer == nil {
		return buffer.Bytes(), nil
	}

	return ci.getCpuInfo(ci.getCpuSets()), nil
}

func (ci CpuInfoFile) getCpuInfo(cpuIDs []int) []byte {
	buffer.Reset()

	if len(cpuIDs) == 0 {
		for _, info := range cpuinfo {
			buffer.WriteString(info)
		}
	} else {
		for i, id := range cpuIDs {
			info, found := cpuinfo[id]
			if found {
				buffer.WriteString(replacer.ReplaceAllString(info,
					fmt.Sprintf("%-16s: %d", "processor", i)))
			}
		}
	}

	return buffer.Bytes()
}

func (ci CpuInfoFile) getCpuSets() []int {
	var (
		err               error
		rawContent        []byte
		content           string
		tmpArray          []int = make([]int, len(cpuinfo))
		cpuID, begin, end uint64
	)

	rawContent, err = ioutil.ReadFile(filepath.Join(ci.cgroupdir, "cpuset.cpus"))
	if err != nil {
		logrus.Debugf("Fail to read %s/cpuset.cpus with message %v", ci.cgroupdir, err)
	}

	content = strings.TrimSpace(string(rawContent))
	count := 0
	for _, split := range strings.Split(content, ",") {
		idRange := strings.Split(split, "-")
		// we do not check the error after calling parseUnit, because
		// cgroup has done it for us
		if len(idRange) == 1 {
			cpuID, _ = parseUint(idRange[0], 10, 32)
			tmpArray[count] = int(cpuID)
			count++
		} else if len(idRange) == 2 {
			begin, _ = parseUint(idRange[0], 10, 32)
			end, _ = parseUint(idRange[1], 10, 32)
			for i := int(begin); i <= int(end); i++ {
				tmpArray[count] = i
				count++
			}
		}
	}

	cpuIDs := tmpArray[:count]
	sort.Ints(cpuIDs)

	return cpuIDs
}

func init() {
	if runtime.GOOS == "linux" {
		rawContent, err := ioutil.ReadFile("/proc/cpuinfo")
		if err != nil {
			return
		}

		count := 0
		buffer.Reset()
		for _, line := range strings.Split(string(rawContent), "\n") {
			if len(line) == 0 {
				cpuinfo[count] = buffer.String()
				count++
				buffer.Reset()
			}

			buffer.WriteString(line)
			buffer.WriteString("\n")
		}

		replacer, err = regexp.Compile(pattern)
		if err != nil {
			logrus.Debugf("Compile %s failed with %v", pattern, err)
			return
		}
	}
}
