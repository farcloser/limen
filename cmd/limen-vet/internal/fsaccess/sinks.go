package fsaccess

// sinks are the functions that resolve a path on the operating system's file
// system, by package. Methods are not listed: an *os.File or an *os.Root only
// exists once one of these opened it.
var sinks = map[string]map[string]bool{ //nolint:gochecknoglobals // read-only table.
	"os": set(
		"Chdir", "Chmod", "Chown", "Chtimes", "CopyFS", "Create", "CreateTemp", "DirFS",
		"Lchown", "Link", "Lstat", "Mkdir", "MkdirAll", "MkdirTemp", "Open", "OpenFile",
		"OpenInRoot", "OpenRoot", "ReadDir", "ReadFile", "Readlink", "Remove", "RemoveAll",
		"Rename", "Stat", "Symlink", "Truncate", "WriteFile",
	),
	"io/ioutil":     set("ReadDir", "ReadFile", "TempDir", "TempFile", "WriteFile"),
	"path/filepath": set("EvalSymlinks", "Glob", "Walk", "WalkDir"),
	"syscall":       syscalls,
	// x/sys issues its system calls directly, so nothing reaches syscall's
	// functions to propagate from: its own are listed.
	"golang.org/x/sys/unix":    syscalls,
	"golang.org/x/sys/windows": syscalls,
}

var syscalls = set( //nolint:gochecknoglobals // read-only table.
	// unix
	"Access", "Chdir", "Chmod", "Chown", "Creat", "Faccessat", "Faccessat2", "Fchmodat",
	"Fchownat", "Fstatat", "Lchown", "Link", "Linkat", "Lstat", "Mkdir", "Mkdirat",
	"Mkfifo", "Mkfifoat", "Mknod", "Mknodat", "Open", "Openat", "Openat2", "Readlink",
	"Readlinkat", "Rename", "Renameat", "Renameat2", "Rmdir", "Stat", "Statfs", "Statx",
	"Symlink", "Symlinkat", "Truncate", "Unlink", "Unlinkat", "Utimes", "UtimesNano",
	"UtimesNanoAt",
	// windows
	"CreateDirectory", "CreateFile", "DeleteFile", "FindFirstFile", "GetFileAttributes",
	"GetFileAttributesEx", "MoveFile", "MoveFileEx", "RemoveDirectory",
)

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}

	return m
}
