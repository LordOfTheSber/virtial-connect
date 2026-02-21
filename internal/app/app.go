package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"virtial-connect/internal/config"
	"virtial-connect/internal/editor"
	"virtial-connect/internal/logging"
	"virtial-connect/internal/models"
	"virtial-connect/internal/security"
	"virtial-connect/internal/sshclient"
	"virtial-connect/internal/transfer"
	"virtial-connect/internal/util"
)

type UI struct {
	app fyne.App
	win fyne.Window

	sshClient *sshclient.Client
	transfer  *transfer.Manager
	editor    *editor.Service
	logger    *logging.Logger
	store     *config.Store
	settings  *config.Settings

	hostEntry  *widget.Entry
	portEntry  *widget.Entry
	userEntry  *widget.Entry
	passEntry  *widget.Entry
	keyEntry   *widget.Entry
	ppEntry    *widget.Entry
	profile    *widget.Select
	localFind  *widget.Entry
	remoteFind *widget.Entry

	localPath   string
	remotePath  string
	allLocal    []models.FileEntry
	allRemote   []models.FileEntry
	localItems  []models.FileEntry
	remoteItems []models.FileEntry

	localList  *widget.List
	remoteList *widget.List
	logBox     *widget.Entry
	queueList  *widget.List
	queue      []models.TransferTask

	selectedLocal   int
	selectedRemote  int
	lastLocalTapID  int
	lastRemoteTapID int
	lastLocalTap    time.Time
	lastRemoteTap   time.Time
	counter         atomic.Uint64
}

func New(a fyne.App) *UI {
	u := &UI{app: a, sshClient: &sshclient.Client{}, localPath: mustLocalHome(), remotePath: "/", selectedLocal: -1, selectedRemote: -1}
	u.win = a.NewWindow("Virtial Connect MVP")
	u.win.Resize(fyne.NewSize(1400, 900))
	u.logBox = widget.NewMultiLineEntry()
	u.logBox.Disable()
	u.logger = logging.New(func(line string) { u.runOnUI(func() { u.logBox.SetText(u.logBox.Text + line + "\n") }) })
	if st, err := config.NewStore(); err == nil {
		u.store = st
		u.settings, _ = st.Load()
	}
	if u.settings == nil {
		u.settings = &config.Settings{}
	}
	u.build()
	return u
}

func (u *UI) Show() {
	u.win.CenterOnScreen()
	u.win.Show()
	// Делим старт UI и потенциально долгий локальный листинг,
	// чтобы окно гарантированно появлялось сразу.
	u.refreshLocal()
	u.app.Run()
}

func mustLocalHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "C:\\"
	}
	return h
}

func (u *UI) build() {
	u.hostEntry = widget.NewEntry()
	u.hostEntry.SetPlaceHolder("host")
	u.portEntry = widget.NewEntry()
	u.portEntry.SetText("22")
	u.userEntry = widget.NewEntry()
	u.userEntry.SetPlaceHolder("username")
	u.passEntry = widget.NewPasswordEntry()
	u.passEntry.SetPlaceHolder("password")
	u.keyEntry = widget.NewEntry()
	u.keyEntry.SetPlaceHolder("private key path")
	u.ppEntry = widget.NewPasswordEntry()
	u.ppEntry.SetPlaceHolder("passphrase")
	u.localFind = widget.NewEntry()
	u.localFind.SetPlaceHolder("Search local...")
	u.localFind.OnChanged = func(_ string) { u.applyLocalFilter() }
	u.remoteFind = widget.NewEntry()
	u.remoteFind.SetPlaceHolder("Search remote...")
	u.remoteFind.OnChanged = func(_ string) { u.applyRemoteFilter() }
	u.profile = widget.NewSelect(nil, func(name string) { u.applyProfile(name) })
	u.reloadProfileSelect()

	connectBtn := widget.NewButtonWithIcon("Connect", theme.ConfirmIcon(), u.onConnect)
	disconnectBtn := widget.NewButtonWithIcon("Disconnect", theme.CancelIcon(), func() { u.sshClient.Disconnect(); u.logger.Info("Disconnected") })

	conn := container.NewGridWithColumns(10,
		u.profile, u.hostEntry, u.portEntry, u.userEntry, u.passEntry, u.keyEntry, u.ppEntry,
		connectBtn, disconnectBtn,
	)

	u.localList = widget.NewList(func() int { return len(u.localItems) }, func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(formatEntry(u.localItems[i]))
		})
	u.localList.OnSelected = func(id widget.ListItemID) {
		u.selectedLocal = id
		u.handleLocalDoubleTap(id)
		u.localList.Unselect(id)
	}
	u.localList.OnUnselected = func(widget.ListItemID) {}

	u.remoteList = widget.NewList(func() int { return len(u.remoteItems) }, func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(formatEntry(u.remoteItems[i]))
		})
	u.remoteList.OnSelected = func(id widget.ListItemID) {
		u.selectedRemote = id
		u.handleRemoteDoubleTap(id)
		u.remoteList.Unselect(id)
	}
	u.remoteList.OnUnselected = func(widget.ListItemID) {}

	localPathLabel := widget.NewLabel("Local")
	remotePathLabel := widget.NewLabel("Remote")
	localUp := widget.NewButton("..", func() { u.localPath = filepath.Dir(u.localPath); u.refreshLocal() })
	remoteUp := widget.NewButton("..", func() {
		u.remotePath = util.NormalizeRemotePath(filepath.ToSlash(filepath.Dir(u.remotePath)))
		u.refreshRemote()
	})

	localActions := container.NewGridWithColumns(2,
		widget.NewButton("Open/Up", u.showLocalMenu),
		widget.NewButton("Upload", func() { u.uploadSelectedLocal(false) }),
	)
	localPanel := container.NewBorder(container.NewBorder(u.localFind, localActions, localPathLabel, localUp, widget.NewLabelWithStyle(u.localPath, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})), nil, nil, nil, u.localList)
	remoteActions := container.NewGridWithColumns(5,
		widget.NewButton("Open/Up", u.showRemoteMenu),
		widget.NewButton("Download", func() { u.downloadSelectedRemote() }),
		widget.NewButton("Edit", func() { u.editSelectedRemote() }),
		widget.NewButton("Rename", func() { u.renameSelectedRemote() }),
		widget.NewButton("Delete", func() { u.deleteSelectedRemote() }),
	)
	remotePanel := container.NewBorder(container.NewBorder(u.remoteFind, remoteActions, remotePathLabel, remoteUp, widget.NewLabelWithStyle(u.remotePath, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})), nil, nil, nil, u.remoteList)

	filePanels := container.NewHSplit(localPanel, remotePanel)
	filePanels.Offset = 0.5

	u.queueList = widget.NewList(func() int { return len(u.queue) }, func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			t := u.queue[i]
			o.(*widget.Label).SetText(fmt.Sprintf("%s %s -> %s [%s] %d/%d speed %.1f KB/s ETA %s %s", t.ID, t.SourcePath, t.DestPath, t.Status, t.DoneBytes, t.TotalBytes, t.SpeedBps/1024, t.ETA.Truncate(time.Second), t.Error))
		})

	bottom := container.NewVSplit(container.NewBorder(widget.NewLabel("Transfer queue"), nil, nil, nil, u.queueList), container.NewBorder(widget.NewLabel("Logs"), nil, nil, nil, container.NewVScroll(u.logBox)))
	bottom.Offset = 0.45

	content := container.NewBorder(conn, bottom, nil, nil, filePanels)
	u.win.SetContent(content)
}

func formatEntry(e models.FileEntry) string {
	kind := "F"
	if e.IsDir {
		kind = "D"
	}
	if e.IsLink {
		kind = "L"
	}
	return fmt.Sprintf("[%s] %-32s %12d %s %s", kind, e.Name, e.Size, e.ModTime.Format("2006-01-02 15:04"), e.Mode)
}

func (u *UI) onConnect() {
	port, _ := strconv.Atoi(strings.TrimSpace(u.portEntry.Text))
	host := strings.TrimSpace(u.hostEntry.Text)
	user := strings.TrimSpace(u.userEntry.Text)
	pass := u.passEntry.Text
	key := strings.TrimSpace(u.keyEntry.Text)
	phrase := u.ppEntry.Text
	cfgDir, _ := os.UserConfigDir()

	u.logger.Info("Connecting to %s...", host)
	go func() {
		err := u.sshClient.Connect(sshclient.ConnectConfig{
			Host: host, Port: port, Username: user,
			Password: pass, PrivateKey: key, Passphrase: phrase,
			KnownHosts: filepath.Join(cfgDir, "virtial-connect", "known_hosts"), ConnectTimout: 8 * time.Second,
			OnUnknownHost: func(host, fp string) (bool, error) {
				decisionCh := make(chan bool, 1)
				u.runOnUI(func() {
					dialog.ShowConfirm("Unknown host", fmt.Sprintf("%s fingerprint %s\nTrust this host?", host, fp), func(b bool) {
						decisionCh <- b
					}, u.win)
				})
				return <-decisionCh, nil
			},
		})
		if err != nil {
			u.logger.Error("Connect failed: %v", err)
			u.runOnUI(func() { dialog.ShowError(err, u.win) })
			return
		}

		u.logger.Info("Connected to %s", host)
		u.transfer = transfer.New(transfer.NewSFTPFS(u.sshClient.SFTP()), 3, func(task models.TransferTask) {
			u.runOnUI(func() { u.upsertTask(task) })
		})
		u.editor = editor.New(u.sshClient.SFTP())
		u.saveCurrentProfile()
		u.runOnUI(func() { u.refreshRemote() })
	}()
}

func (u *UI) refreshLocal() {
	entries, err := os.ReadDir(u.localPath)
	if err != nil {
		u.logger.Error("local list: %v", err)
		return
	}
	list := make([]models.FileEntry, 0, len(entries)+1)
	if u.localPath != filepath.VolumeName(u.localPath)+`\` {
		list = append(list, models.FileEntry{Name: "..", Path: filepath.Dir(u.localPath), IsDir: true})
	}
	for _, e := range entries {
		info, _ := e.Info()
		item := models.FileEntry{Name: e.Name(), Path: filepath.Join(u.localPath, e.Name()), IsDir: e.IsDir()}
		if info != nil {
			item.Size = info.Size()
			item.ModTime = info.ModTime()
			item.Mode = info.Mode().String()
		}
		list = append(list, item)
	}
	u.localItems = list
	u.allLocal = list
	u.applyLocalFilter()
}

func (u *UI) refreshRemote() {
	if !u.sshClient.IsConnected() {
		return
	}
	items, err := u.sshClient.ListRemote(u.remotePath)
	if err != nil {
		u.logger.Error("remote list: %v", err)
		return
	}
	u.allRemote = items
	u.applyRemoteFilter()
}

func (u *UI) showLocalMenu() {
	if u.selectedLocal < 0 || u.selectedLocal >= len(u.localItems) {
		return
	}
	sel := u.localItems[u.selectedLocal]
	if sel.Name == ".." {
		u.localPath = sel.Path
		u.refreshLocal()
		return
	}
	if sel.IsDir {
		u.localPath = sel.Path
		u.refreshLocal()
		return
	}
	if !u.sshClient.IsConnected() {
		return
	}
	u.enqueue(models.Upload, sel.Path, util.JoinRemote(u.remotePath, sel.Name), sel.Size)
}

func (u *UI) showRemoteMenu() {
	if u.selectedRemote < 0 || u.selectedRemote >= len(u.remoteItems) {
		return
	}
	sel := u.remoteItems[u.selectedRemote]
	if sel.Name == ".." {
		u.remotePath = sel.Path
		u.refreshRemote()
		return
	}
	if sel.IsDir {
		u.remotePath = sel.Path
		u.refreshRemote()
		return
	}

}

func (u *UI) runOnUI(fn func()) {
	if fn != nil {
		fn()
	}
}

func (u *UI) applyLocalFilter() {
	q := strings.ToLower(strings.TrimSpace(u.localFind.Text))
	normQ := normalizeSearch(q)
	if q == "" {
		u.localItems = u.allLocal
		u.localList.Refresh()
		return
	}
	out := make([]models.FileEntry, 0, len(u.allLocal))
	for _, item := range u.allLocal {
		name := strings.ToLower(item.Name)
		if item.Name == ".." || strings.Contains(name, q) || strings.Contains(normalizeSearch(name), normQ) {
			out = append(out, item)
		}
	}
	u.localItems = out
	u.localList.Refresh()
}

func (u *UI) applyRemoteFilter() {
	q := strings.ToLower(strings.TrimSpace(u.remoteFind.Text))
	normQ := normalizeSearch(q)
	if q == "" {
		u.remoteItems = u.allRemote
		u.remoteList.Refresh()
		return
	}
	out := make([]models.FileEntry, 0, len(u.allRemote))
	for _, item := range u.allRemote {
		name := strings.ToLower(item.Name)
		if item.Name == ".." || strings.Contains(name, q) || strings.Contains(normalizeSearch(name), normQ) {
			out = append(out, item)
		}
	}
	u.remoteItems = out
	u.remoteList.Refresh()
}

func (u *UI) handleLocalDoubleTap(id int) {
	if id < 0 || id >= len(u.localItems) {
		return
	}
	now := time.Now()
	if u.lastLocalTapID == id && now.Sub(u.lastLocalTap) < 700*time.Millisecond {
		item := u.localItems[id]
		if item.IsDir || item.Name == ".." {
			u.localPath = item.Path
			u.refreshLocal()
		} else {
			u.openLocalFile(item.Path)
		}
	}
	u.lastLocalTapID = id
	u.lastLocalTap = now
}

func (u *UI) handleRemoteDoubleTap(id int) {
	if id < 0 || id >= len(u.remoteItems) {
		return
	}
	now := time.Now()
	if u.lastRemoteTapID == id && now.Sub(u.lastRemoteTap) < 700*time.Millisecond {
		item := u.remoteItems[id]
		if item.IsDir || item.Name == ".." {
			u.remotePath = item.Path
			u.refreshRemote()
		} else if u.editor != nil {
			u.openEditor(item)
		}
	}
	u.lastRemoteTapID = id
	u.lastRemoteTap = now
}

func (u *UI) openLocalFile(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/C", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		u.logger.Error("open local file: %v", err)
	}
}

func normalizeSearch(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (u *UI) reloadProfileSelect() {
	if u.profile == nil || u.settings == nil {
		return
	}
	options := make([]string, 0, len(u.settings.Profiles))
	for _, p := range u.settings.Profiles {
		if p.Name != "" {
			options = append(options, p.Name)
		}
	}
	u.profile.Options = options
	u.profile.Refresh()
}

func (u *UI) applyProfile(name string) {
	for _, p := range u.settings.Profiles {
		if p.Name != name {
			continue
		}
		u.hostEntry.SetText(p.Host)
		u.portEntry.SetText(strconv.Itoa(p.Port))
		u.userEntry.SetText(p.Username)
		u.keyEntry.SetText(p.PrivateKeyPath)
		if dec, err := security.DecryptString(p.PasswordEnc); err == nil {
			u.passEntry.SetText(dec)
		}
		if dec, err := security.DecryptString(p.PassphraseEnc); err == nil {
			u.ppEntry.SetText(dec)
		}
		if p.LastLocalPath != "" {
			u.localPath = p.LastLocalPath
			u.refreshLocal()
		}
		if p.LastRemotePath != "" {
			u.remotePath = p.LastRemotePath
		}
		break
	}
}

func (u *UI) saveCurrentProfile() {
	if u.store == nil || u.settings == nil {
		return
	}
	name := strings.TrimSpace(fmt.Sprintf("%s@%s:%s", u.userEntry.Text, u.hostEntry.Text, u.portEntry.Text))
	port, _ := strconv.Atoi(strings.TrimSpace(u.portEntry.Text))
	passEnc, _ := security.EncryptString(u.passEntry.Text)
	phraseEnc, _ := security.EncryptString(u.ppEntry.Text)
	p := models.ConnectionProfile{
		Name:            name,
		Host:            strings.TrimSpace(u.hostEntry.Text),
		Port:            port,
		Username:        strings.TrimSpace(u.userEntry.Text),
		PrivateKeyPath:  strings.TrimSpace(u.keyEntry.Text),
		PasswordEnc:     passEnc,
		PassphraseEnc:   phraseEnc,
		LastLocalPath:   u.localPath,
		LastRemotePath:  u.remotePath,
		LastConnectedAt: time.Now(),
	}
	replaced := false
	for i := range u.settings.Profiles {
		if u.settings.Profiles[i].Name == p.Name {
			u.settings.Profiles[i] = p
			replaced = true
			break
		}
	}
	if !replaced {
		u.settings.Profiles = append(u.settings.Profiles, p)
	}
	if err := u.store.Save(u.settings); err != nil {
		u.logger.Error("save profiles: %v", err)
		return
	}
	u.runOnUI(func() {
		u.reloadProfileSelect()
		u.profile.SetSelected(p.Name)
	})
}

func (u *UI) selectedLocalItem() (models.FileEntry, bool) {
	if u.selectedLocal < 0 || u.selectedLocal >= len(u.localItems) {
		return models.FileEntry{}, false
	}
	return u.localItems[u.selectedLocal], true
}

func (u *UI) selectedRemoteItem() (models.FileEntry, bool) {
	if u.selectedRemote < 0 || u.selectedRemote >= len(u.remoteItems) {
		return models.FileEntry{}, false
	}
	return u.remoteItems[u.selectedRemote], true
}

func (u *UI) uploadSelectedLocal(allowDir bool) {
	sel, ok := u.selectedLocalItem()
	if !ok || !u.sshClient.IsConnected() || sel.Name == ".." {
		return
	}
	if sel.IsDir && !allowDir {
		return
	}
	u.enqueue(models.Upload, sel.Path, util.JoinRemote(u.remotePath, sel.Name), sel.Size)
}

func (u *UI) downloadSelectedRemote() {
	sel, ok := u.selectedRemoteItem()
	if !ok || sel.Name == ".." {
		return
	}
	u.enqueue(models.Download, sel.Path, filepath.Join(u.localPath, sel.Name), sel.Size)
}

func (u *UI) editSelectedRemote() {
	sel, ok := u.selectedRemoteItem()
	if !ok || sel.IsDir || sel.Name == ".." {
		return
	}
	u.openEditor(sel)
}

func (u *UI) renameSelectedRemote() {
	sel, ok := u.selectedRemoteItem()
	if !ok || sel.Name == ".." {
		return
	}
	u.renameRemote(sel)
}

func (u *UI) deleteSelectedRemote() {
	sel, ok := u.selectedRemoteItem()
	if !ok || sel.Name == ".." {
		return
	}
	u.deleteRemote(sel)
}

func (u *UI) enqueue(direction models.TransferDirection, src, dst string, total int64) {
	if u.transfer == nil {
		return
	}
	id := fmt.Sprintf("task-%d", u.counter.Add(1))
	u.transfer.Enqueue(&models.TransferTask{ID: id, Direction: direction, SourcePath: src, DestPath: dst, TotalBytes: total})
}

func (u *UI) upsertTask(task models.TransferTask) {
	for i := range u.queue {
		if u.queue[i].ID == task.ID {
			u.queue[i] = task
			u.queueList.Refresh()
			return
		}
	}
	u.queue = append(u.queue, task)
	u.queueList.Refresh()
	if task.Status == models.TransferCompleted && task.Direction == models.Download {
		u.refreshLocal()
	}
	if task.Status == models.TransferCompleted && task.Direction == models.Upload {
		u.refreshRemote()
	}
}

func (u *UI) openEditor(item models.FileEntry) {
	opened, content, err := u.editor.Open(item.Path, 10*1024*1024)
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	w := u.app.NewWindow("Edit: " + item.Name)
	text := widget.NewMultiLineEntry()
	text.SetText(string(content))
	if opened.ReadOnly {
		text.Disable()
	}
	search := widget.NewEntry()
	repl := widget.NewEntry()
	findBtn := widget.NewButton("Find next", func() {
		idx := strings.Index(text.Text, search.Text)
		if idx >= 0 {
			dialog.ShowInformation("Find", fmt.Sprintf("Found at position %d", idx), w)
		}
	})
	replaceBtn := widget.NewButton("Replace all", func() { text.SetText(strings.ReplaceAll(text.Text, search.Text, repl.Text)) })
	saveBtn := widget.NewButton("Save", func() {
		err := u.editor.SaveAtomic(opened, []byte(text.Text), false)
		if err != nil {
			if strings.Contains(err.Error(), "changed") {
				dialog.ShowConfirm("Conflict", "File changed remotely. Overwrite?", func(ok bool) {
					if ok {
						_ = u.editor.SaveAtomic(opened, []byte(text.Text), true)
					}
				}, w)
				return
			}
			dialog.ShowError(err, w)
			return
		}
		dialog.ShowInformation("Saved", "File saved atomically", w)
	})
	toolbar := container.NewGridWithColumns(6, widget.NewLabel("Mode: "+editor.BasicSyntaxHint(item.Name)), search, repl, findBtn, replaceBtn, saveBtn)
	w.SetContent(container.NewBorder(toolbar, nil, nil, nil, text))
	w.Resize(fyne.NewSize(800, 600))
	w.Show()
}

func (u *UI) renameRemote(item models.FileEntry) {
	entry := widget.NewEntry()
	entry.SetText(item.Name)
	dialog.ShowForm("Rename", "OK", "Cancel", []*widget.FormItem{{Text: "New name", Widget: entry}}, func(ok bool) {
		if !ok {
			return
		}
		newPath := util.JoinRemote(u.remotePath, entry.Text)
		if err := u.sshClient.SFTP().Rename(item.Path, newPath); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		u.refreshRemote()
	}, u.win)
}

func (u *UI) deleteRemote(item models.FileEntry) {
	dialog.ShowConfirm("Delete", "Delete "+item.Name+"?", func(ok bool) {
		if !ok {
			return
		}
		var err error
		if item.IsDir {
			err = u.sshClient.SFTP().RemoveDirectory(item.Path)
		} else {
			err = u.sshClient.SFTP().Remove(item.Path)
		}
		if err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		u.refreshRemote()
	}, u.win)
}
