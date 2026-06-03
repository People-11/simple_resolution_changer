package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shcore   = syscall.NewLazyDLL("shcore.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	// API functions
	procCreateWindowEx         = user32.NewProc("CreateWindowExW")
	procDefWindowProc          = user32.NewProc("DefWindowProcW")
	procRegisterClassEx        = user32.NewProc("RegisterClassExW")
	procGetMessage             = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessage        = user32.NewProc("DispatchMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procGetModuleHandle        = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentProcess      = kernel32.NewProc("GetCurrentProcess")
	procLoadCursor             = user32.NewProc("LoadCursorW")
	procGetStockObject         = gdi32.NewProc("GetStockObject")
	procCreateFont             = gdi32.NewProc("CreateFontW")
	procEnumWindows            = user32.NewProc("EnumWindows")
	procGetWindowText          = user32.NewProc("GetWindowTextW")
	procGetWindowTextLength    = user32.NewProc("GetWindowTextLengthW")
	procIsWindowVisible        = user32.NewProc("IsWindowVisible")
	procGetWindowLong          = user32.NewProc("GetWindowLongW")
	procSetWindowLong          = user32.NewProc("SetWindowLongW")
	procSetWindowPos           = user32.NewProc("SetWindowPos")
	procSetProcessDpiAwareness = shcore.NewProc("SetProcessDpiAwareness")
	procSendMessage            = user32.NewProc("SendMessageW")
	procMessageBox             = user32.NewProc("MessageBoxW")
	procSetWindowText          = user32.NewProc("SetWindowTextW")
	procPostMessage            = user32.NewProc("PostMessageW")
	procEnumDisplayMonitors    = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfo         = user32.NewProc("GetMonitorInfoW")
	procShellExecute           = shell32.NewProc("ShellExecuteW")
	procOpenProcessToken       = advapi32.NewProc("OpenProcessToken")
	procGetTokenInformation    = advapi32.NewProc("GetTokenInformation")
	procCloseHandle            = kernel32.NewProc("CloseHandle")
)

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_TABSTOP          = 0x00010000
	WS_CAPTION          = 0x00C00000
	WS_THICKFRAME       = 0x00040000
	WS_BORDER           = 0x00800000
	SWP_SHOWWINDOW      = 0x0040
	WM_DESTROY          = 0x0002
	WM_COMMAND          = 0x0111
	WM_CLOSE            = 0x0010
	WM_SETFONT          = 0x0030
	CB_ADDSTRING        = 0x0143
	CB_GETCURSEL        = 0x0147
	CB_RESETCONTENT     = 0x014B
	CB_SETCURSEL        = 0x014E
	BM_GETCHECK         = 0x00F0
	BM_SETCHECK         = 0x00F1
	BST_CHECKED         = 0x0001
	CBS_DROPDOWNLIST    = 0x0003
	BS_PUSHBUTTON       = 0x0000
	BS_AUTOCHECKBOX     = 0x0003
	ES_LEFT             = 0x0000
	WHITE_BRUSH         = 0
	IDC_ARROW           = 32512
	DEFAULT_CHARSET     = 1
	CLIP_DEFAULT_PRECIS = 0
	CLEARTYPE_QUALITY   = 5
	DEFAULT_PITCH       = 0
	BN_CLICKED          = 0
	GWL_STYLE           = -16
	ID_WINDOW_COMBO     = 1001
	ID_MONITOR_COMBO    = 1002
	ID_WIDTH_EDIT       = 1003
	ID_HEIGHT_EDIT      = 1004
	ID_BORDERLESS_CHECK = 1005
	ID_REFRESH_BTN      = 1006
	ID_RESIZE_BTN       = 1007
	WM_APP_REFRESH_DONE = 0x8001
	TOKEN_QUERY         = 0x0008
	TokenElevation      = 20
	SW_SHOWNORMAL       = 1
)

type (
	WNDCLASSEX struct {
		CbSize, Style                            uint32
		LpfnWndProc                              uintptr
		CbClsExtra, CbWndExtra                   int32
		HInstance, HIcon, HCursor, HbrBackground uintptr
		LpszMenuName, LpszClassName              *uint16
		HIconSm                                  uintptr
	}
	MSG struct {
		Hwnd           uintptr
		Message        uint32
		WParam, LParam uintptr
		Time           uint32
		Pt             struct{ X, Y int32 }
	}
	WindowInfo struct {
		Handle uintptr
		Title  string
	}
	RECT        struct{ Left, Top, Right, Bottom int32 }
	MONITORINFO struct {
		CbSize            uint32
		RcMonitor, RcWork RECT
		DwFlags           uint32
	}
	MonitorInfo     struct{ Index, Left, Top, Width, Height int }
	TOKEN_ELEVATION struct{ TokenIsElevated uint32 }
	Config          struct {
		Width       string `json:"width"`
		Height      string `json:"height"`
		Monitor     string `json:"monitor"`
		WindowTitle string `json:"window_title"`
		Borderless  bool   `json:"borderless"`
	}
)

var (
	windows                                                []WindowInfo
	monitors                                               []MonitorInfo
	mainWindow, windowComboBox, monitorComboBox, widthEdit uintptr
	heightEdit, borderlessCheck, hInstance                 uintptr
	uiFont, titleFont                                      uintptr
	configFile                                             string
	isRefreshing                                           bool
	windowsLock                                            sync.Mutex
	lastSavedMonitor, lastSavedWindow                      string
)

func initConfigPath() {
	dir, _ := os.UserConfigDir()
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		dir = local
	}
	appDir := dir + "\\SimpleResolutionChanger"
	os.MkdirAll(appDir, 0755)
	configFile = appDir + "\\config.json"
}

func u16(s string) uintptr {
	p, _ := syscall.UTF16PtrFromString(s)
	return uintptr(unsafe.Pointer(p))
}

func sendMsg(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	ret, _, _ := procSendMessage.Call(hwnd, uintptr(msg), wp, lp)
	return ret
}

func createFont(face string) uintptr {
	font, _, _ := procCreateFont.Call(
		uintptr(^uint32(15)+1), 0, 0, 0, 400, 0, 0, 0,
		DEFAULT_CHARSET, 0, CLIP_DEFAULT_PRECIS, CLEARTYPE_QUALITY, DEFAULT_PITCH,
		u16(face),
	)
	return font
}

func createControl(class, text string, style, x, y, w, h, id, font uintptr) uintptr {
	hwnd, _, _ := procCreateWindowEx.Call(0, u16(class), u16(text), style, x, y, w, h, mainWindow, id, hInstance, 0)
	if hwnd != 0 && font != 0 {
		sendMsg(hwnd, WM_SETFONT, font, 1)
	}
	return hwnd
}

func saveConfig(width, height, monitor, windowTitle string, borderless bool) {
	cfg := Config{
		Width:       width,
		Height:      height,
		Monitor:     monitor,
		WindowTitle: windowTitle,
		Borderless:  borderless,
	}
	if data, err := json.MarshalIndent(cfg, "", "  "); err == nil {
		os.WriteFile(configFile, data, 0644)
	}
}

func loadConfig() (c Config) {
	c = Config{Width: "800", Height: "600", Monitor: "Monitor 1", Borderless: true}
	if data, err := os.ReadFile(configFile); err == nil {
		json.Unmarshal(data, &c)
	}
	return
}

func showError(msg string) {
	procMessageBox.Call(mainWindow, u16(msg), u16("Error"), 0x30)
}

func isRunningAsAdmin() bool {
	var token uintptr
	process, _, _ := procGetCurrentProcess.Call()
	if r, _, _ := procOpenProcessToken.Call(process, TOKEN_QUERY, uintptr(unsafe.Pointer(&token))); r == 0 {
		return false
	}
	defer procCloseHandle.Call(token)

	var elevation TOKEN_ELEVATION
	var returned uint32
	if r, _, _ := procGetTokenInformation.Call(token, TokenElevation, uintptr(unsafe.Pointer(&elevation)), unsafe.Sizeof(elevation), uintptr(unsafe.Pointer(&returned))); r == 0 {
		return false
	}
	return elevation.TokenIsElevated != 0
}

func relaunchAsAdmin() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	r, _, _ := procShellExecute.Call(0, u16("runas"), u16(exe), 0, 0, SW_SHOWNORMAL)
	return r > 32
}

func requireAdmin() bool {
	return isRunningAsAdmin() || !relaunchAsAdmin()
}

func setText(hwnd uintptr, text string) {
	procSetWindowText.Call(hwnd, u16(text))
}

func getText(hwnd uintptr) string {
	l, _, _ := procGetWindowTextLength.Call(hwnd)
	if l == 0 {
		return ""
	}
	buf := make([]uint16, l+1)
	procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func getMonitorInfo() (res []MonitorInfo) {
	idx := 0
	enumFunc := func(h, hdc, lprc, data uintptr) uintptr {
		var mi MONITORINFO
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		if r, _, _ := procGetMonitorInfo.Call(h, uintptr(unsafe.Pointer(&mi))); r != 0 {
			res = append(res, MonitorInfo{
				Index:  idx + 1,
				Left:   int(mi.RcMonitor.Left),
				Top:    int(mi.RcMonitor.Top),
				Width:  int(mi.RcMonitor.Right - mi.RcMonitor.Left),
				Height: int(mi.RcMonitor.Bottom - mi.RcMonitor.Top),
			})
		}
		idx++
		return 1
	}
	procEnumDisplayMonitors.Call(0, 0, syscall.NewCallback(enumFunc), 0)
	return
}

func updateUILists() {
	windowsLock.Lock()
	defer windowsLock.Unlock()
	sendMsg(windowComboBox, CB_RESETCONTENT, 0, 0)
	winSel := -1
	for i, win := range windows {
		if win.Title == lastSavedWindow {
			winSel = i
		}
		sendMsg(windowComboBox, CB_ADDSTRING, 0, u16(win.Title))
	}
	if winSel >= 0 {
		sendMsg(windowComboBox, CB_SETCURSEL, uintptr(winSel), 0)
	}

	sendMsg(monitorComboBox, CB_RESETCONTENT, 0, 0)
	monitors = getMonitorInfo()
	sel := 0
	for i, m := range monitors {
		monitorName := fmt.Sprintf("Monitor %d", m.Index)
		if monitorName == lastSavedMonitor {
			sel = i
		}
		sendMsg(monitorComboBox, CB_ADDSTRING, 0, u16(fmt.Sprintf("%s (%dx%d)", monitorName, m.Width, m.Height)))
	}
	if len(monitors) > 0 {
		sendMsg(monitorComboBox, CB_SETCURSEL, uintptr(sel), 0)
	}
}

func getWindows() (res []WindowInfo) {
	var hws []uintptr
	procEnumWindows.Call(syscall.NewCallback(func(h, lp uintptr) uintptr {
		hws = append(hws, h)
		return 1
	}), 0)
	for _, h := range hws {
		if v, _, _ := procIsWindowVisible.Call(h); v != 0 {
			if t := getText(h); t != "" && t != "Window Resizer" {
				res = append(res, WindowInfo{h, t})
			}
		}
	}
	return
}

func refreshLists() {
	if isRefreshing {
		return
	}
	isRefreshing = true
	defer func() { isRefreshing = false }()
	local := getWindows()
	windowsLock.Lock()
	windows = local
	windowsLock.Unlock()
	procPostMessage.Call(mainWindow, WM_APP_REFRESH_DONE, 0, 0)
}

func performResize() {
	windowsLock.Lock()
	local := append([]WindowInfo{}, windows...)
	windowsLock.Unlock()
	winIdx := int(sendMsg(windowComboBox, CB_GETCURSEL, 0, 0))
	monIdx := int(sendMsg(monitorComboBox, CB_GETCURSEL, 0, 0))
	wStr, hStr := getText(widthEdit), getText(heightEdit)
	w, errW := strconv.Atoi(wStr)
	h, errH := strconv.Atoi(hStr)
	if winIdx < 0 || winIdx >= len(local) || monIdx < 0 || monIdx >= len(monitors) || errW != nil || errH != nil || w <= 0 || h <= 0 {
		showError("Invalid selection or dimensions.")
		return
	}
	hwnd, title, b := local[winIdx].Handle, local[winIdx].Title, sendMsg(borderlessCheck, BM_GETCHECK, 0, 0) == BST_CHECKED

	// If the handle is invalid, auto-refresh and try to find it by title
	if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
		newList := getWindows()
		found := false
		for _, win := range newList {
			if win.Title == title {
				hwnd, found = win.Handle, true
				break
			}
		}
		// Sync the new list back to global and UI so the user stays in sync
		windowsLock.Lock()
		windows = newList
		windowsLock.Unlock()
		procPostMessage.Call(mainWindow, WM_APP_REFRESH_DONE, 0, 0)

		if !found {
			showError("Window \"" + title + "\" not found even after auto-refresh.")
			return
		}
	}

	monitor := monitors[monIdx]
	x, y := monitor.Left+(monitor.Width-w)/2, monitor.Top+(monitor.Height-h)/2
	style, _, _ := procGetWindowLong.Call(hwnd, ^uintptr(15))
	if b {
		style &^= (WS_CAPTION | WS_THICKFRAME)
	} else {
		style |= (WS_CAPTION | WS_THICKFRAME)
	}
	procSetWindowLong.Call(hwnd, ^uintptr(15), style)
	if r, _, _ := procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), SWP_SHOWWINDOW); r != 0 {
		lastSavedMonitor = fmt.Sprintf("Monitor %d", monitor.Index)
		lastSavedWindow = title
		saveConfig(wStr, hStr, lastSavedMonitor, lastSavedWindow, b)
	} else {
		showError("Failed to resize window.")
	}
}

func wndProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case WM_DESTROY, WM_CLOSE:
		procPostQuitMessage.Call(0)
		return 0
	case WM_APP_REFRESH_DONE:
		updateUILists()
	case WM_COMMAND:
		if (wp >> 16) == BN_CLICKED {
			switch wp & 0xFFFF {
			case ID_REFRESH_BTN:
				go refreshLists()
			case ID_RESIZE_BTN:
				performResize()
			}
		}
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wp, lp)
	return r
}

func createControls() {
	createControl("STATIC", "Target Window:", WS_VISIBLE|WS_CHILD, 20, 20, 150, 30, 0, uiFont)
	windowComboBox = createControl("COMBOBOX", "", WS_VISIBLE|WS_CHILD|WS_TABSTOP|CBS_DROPDOWNLIST, 20, 55, 460, 250, ID_WINDOW_COMBO, titleFont)
	createControl("STATIC", "Target Monitor:", WS_VISIBLE|WS_CHILD, 20, 105, 150, 30, 0, uiFont)
	monitorComboBox = createControl("COMBOBOX", "", WS_VISIBLE|WS_CHILD|WS_TABSTOP|CBS_DROPDOWNLIST, 20, 140, 460, 250, ID_MONITOR_COMBO, uiFont)
	createControl("BUTTON", "Refresh Lists", WS_VISIBLE|WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON, 20, 190, 150, 40, ID_REFRESH_BTN, uiFont)
	createControl("STATIC", "Width:", WS_VISIBLE|WS_CHILD, 20, 250, 80, 30, 0, uiFont)
	widthEdit = createControl("EDIT", "", WS_VISIBLE|WS_CHILD|WS_TABSTOP|WS_BORDER|ES_LEFT, 110, 248, 120, 30, ID_WIDTH_EDIT, uiFont)
	createControl("STATIC", "Height:", WS_VISIBLE|WS_CHILD, 260, 250, 80, 30, 0, uiFont)
	heightEdit = createControl("EDIT", "", WS_VISIBLE|WS_CHILD|WS_TABSTOP|WS_BORDER|ES_LEFT, 350, 248, 120, 30, ID_HEIGHT_EDIT, uiFont)
	borderlessCheck = createControl("BUTTON", "Borderless", WS_VISIBLE|WS_CHILD|WS_TABSTOP|BS_AUTOCHECKBOX, 20, 300, 150, 30, ID_BORDERLESS_CHECK, uiFont)
	createControl("BUTTON", "Resize Window", WS_VISIBLE|WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON, 20, 350, 180, 40, ID_RESIZE_BTN, uiFont)
}

func main() {
	runtime.LockOSThread()
	if !requireAdmin() {
		return
	}
	initConfigPath()
	procSetProcessDpiAwareness.Call(1)
	hInstance, _, _ = procGetModuleHandle.Call(0)
	cls := u16("WindowResizerClass")
	var wc WNDCLASSEX
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = syscall.NewCallback(wndProc)
	wc.HInstance = hInstance
	wc.HCursor, _, _ = procLoadCursor.Call(0, IDC_ARROW)
	wc.HbrBackground, _, _ = procGetStockObject.Call(WHITE_BRUSH)
	wc.LpszClassName = (*uint16)(unsafe.Pointer(cls))
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	mainWindow, _, _ = procCreateWindowEx.Call(0, cls, u16("Window Resizer"), WS_OVERLAPPEDWINDOW|WS_VISIBLE, 200, 150, 520, 460, 0, 0, hInstance, 0)
	if mainWindow == 0 {
		return
	}
	uiFont = createFont("Segoe UI")
	titleFont = createFont("Segoe UI Symbol")
	createControls()
	cfg := loadConfig()
	lastSavedMonitor = cfg.Monitor
	lastSavedWindow = cfg.WindowTitle
	setText(widthEdit, cfg.Width)
	setText(heightEdit, cfg.Height)
	if cfg.Borderless {
		sendMsg(borderlessCheck, BM_SETCHECK, BST_CHECKED, 0)
	}
	go refreshLists()
	var msg MSG
	for {
		if r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0); r == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
