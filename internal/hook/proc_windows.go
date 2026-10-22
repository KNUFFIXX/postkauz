//go:build windows

package hook

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tree hält den Hook samt Kindern in einem Job-Objekt (Windows): Kinder
// erben den Job, TerminateJobObject beendet alle; beim Schließen des
// Handles beendet Windows übrig gebliebene Prozesse ebenfalls
// (JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE).
//
// Der Hook kommt unmittelbar nach dem Start in den Job. Ein Kind, das er in
// diesen Mikrosekunden startet, wäre nicht darin; Programme wie
// powershell.exe brauchen bis zum ersten eigenen Kindprozess um
// Größenordnungen länger. Scheitert die Zuordnung, wird der Hook beendet
// und das Ergebnis ist error (fail closed).
type tree struct {
	job windows.Handle
}

func newTree() (*tree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &tree{job: job}, nil
}

// prepare: kein eigenes Konsolenfenster (der MCP-Server läuft ohne
// sichtbare Konsole; ein Kind bekäme sonst ein neues Fenster).
func (*tree) prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW, HideWindow: true}
}

func (t *tree) attach(p *os.Process) error {
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.AssignProcessToJobObject(t.job, h)
}

// kill beendet alle Prozesse im Job; ohne Job (Zuordnung gescheitert) nur
// den Hook selbst.
func (t *tree) kill(p *os.Process) error {
	if t.job != 0 {
		if err := windows.TerminateJobObject(t.job, 1); err == nil {
			return nil
		}
	}
	if p == nil {
		return nil
	}
	if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

func (t *tree) close() {
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
}

// ownedByUser: Unter Windows liegt %TEMP% im Profil des Nutzers, und jedes
// Temp-Verzeichnis eines Hooks ist nur für seinen Nutzer zugänglich
// (restrictDir); ein fremdes ließe sich ohnehin nicht löschen.
func ownedByUser(os.FileInfo) bool { return true }

// restrictDir setzt eine geschützte Zugriffsliste: Vollzugriff nur für den
// angemeldeten Nutzer (vererbt an Dateien darin), keine geerbten Einträge.
// %TEMP% liegt zwar im Profil des Nutzers, aber geerbte Rechte (etwa von
// einem umgeleiteten TEMP) sollen hier nicht gelten.
func restrictDir(dir string) error {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(u.User.Sid),
		},
	}}, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
