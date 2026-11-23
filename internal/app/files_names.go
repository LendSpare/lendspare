package app

import (
	"os"
	"path"
	"runtime"
	"strings"
)

// Names this computer's filesystem cannot hold, said out loud instead of failing quietly
// (docs/features/29-computer-files.md). A site on Linux happily has `CON.txt`, `a:b.txt`,
// `report.` and both `Photo.jpg` and `photo.jpg`; on Windows the first three cannot be created
// and the last two are one file - writing the second would silently replace the first.

// caseInsensitiveFS: Windows and a default macOS volume treat Photo.jpg and photo.jpg as one.
var caseInsensitiveFS = runtime.GOOS != "linux"

// targetOS is whose naming rules apply (a variable so tests can play Windows on Linux).
var targetOS = runtime.GOOS

var windowsReserved = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true, "conin$": true, "conout$": true}

func init() {
	for _, p := range []string{"com", "lpt"} {
		for i := 0; i <= 9; i++ {
			windowsReserved[p+string(rune('0'+i))] = true
		}
		for _, sup := range []string{"¹", "²", "³"} {
			windowsReserved[p+sup] = true
		}
	}
}

// nameProblem says why `rel` cannot be written on an OS like `goos`, or "" when it can.
func nameProblem(rel, goos string) string {
	for _, part := range strings.Split(rel, "/") {
		if len(part) > 255 {
			return "the name " + clip(part) + " is longer than 255 bytes"
		}
		switch goos {
		case "windows":
			if strings.ContainsAny(part, `<>:"|?*`) || strings.IndexFunc(part, func(r rune) bool { return r < 0x20 }) >= 0 {
				return "the name " + clip(part) + ` holds a character Windows does not allow (< > : " | ? * or a control character)`
			}
			if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
				return "the name " + clip(part) + " ends with a dot or a space, which Windows drops"
			}
			stem := strings.ToLower(strings.TrimRight(part, ". "))
			if i := strings.Index(stem, "."); i >= 0 {
				stem = stem[:i]
			}
			if windowsReserved[strings.TrimSpace(stem)] {
				return "the name " + clip(part) + " is reserved on Windows (CON, PRN, AUX, NUL, COM1-9, LPT1-9)"
			}
		case "darwin":
			if strings.Contains(part, ":") {
				return "the name " + clip(part) + " holds a colon, which macOS does not allow"
			}
		}
	}
	return ""
}

func clip(s string) string {
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return `"` + s + `"`
}

// caseTwin finds an entry in rel's folder whose name differs from rel's only in case - on a
// case-insensitive disk (Windows, a default macOS volume) that is the same file, and writing
// would replace a different one. Returns its real name, or "".
func caseTwin(root *os.Root, rel string) string {
	if !caseInsensitiveFS {
		return "" // case-sensitive: two names, two files
	}
	f, err := root.Open(osRel(path.Dir(rel)))
	if err != nil {
		return ""
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return ""
	}
	base := path.Base(rel)
	for _, n := range names {
		if n != base && strings.EqualFold(n, base) {
			return n
		}
	}
	return ""
}

// checkTargetName: everything that decides whether a name may be written here at all.
func checkTargetName(root *os.Root, rel string, share shareConfig) error {
	if p := nameProblem(rel, targetOS); p != "" {
		return refuse("name_not_allowed", p)
	}
	if twin := caseTwin(root, rel); twin != "" {
		return &opError{Code: "case_collision", Message: "this computer already has " + clip(twin) +
			", which its disk treats as the same name as " + clip(path.Base(rel)) + " - nothing was replaced",
			Extra: map[string]any{"existingName": twin}}
	}
	return checkWritableName(rel, share)
}
