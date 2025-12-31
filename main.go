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
	user32 = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shcore = syscall.NewLazyDLL("shcore.dll")
	gdi32 = syscall.NewLazyDLL("gdi32.dll")
	// API functions
	procCreateWindowEx = user32.NewProc("CreateWindowExW")
	procDefWindowProc = user32.NewProc("DefWindowProcW")
	procRegisterClassEx = user32.NewProc("RegisterClassExW")
	procGetMessage = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage = user32.NewProc("DispatchMessageW")
	procPostQuitMessage = user32.NewProc("PostQuitMessage")
	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
	procLoadCursor = user32.NewProc("LoadCursorW")
	procGetStockObject = gdi32.NewProc("GetStockObject")
	procEnumWindows = user32.NewProc("EnumWindows")
	procGetWindowText = user32.NewProc("GetWindowTextW")
	procGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
	procGetWindowLong = user32.NewProc("GetWindowLongW")
	procSetWindowLong = user32.NewProc("SetWindowLongW")
	procSetWindowPos = user32.NewProc("SetWindowPos")
	procSetProcessDpiAwareness = shcore.NewProc("SetProcessDpiAwareness")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procSendMessage = user32.NewProc("SendMessageW")
	procMessageBox = user32.NewProc("MessageBoxW")
	procSetWindowText = user32.NewProc("SetWindowTextW")
	procPostMessage = user32.NewProc("PostMessageW")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfo = user32.NewProc("GetMonitorInfoW")
)

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_VISIBLE = 0x10000000
	WS_CHILD = 0x40000000
	WS_TABSTOP = 0x00010000
	WS_CAPTION = 0x00C00000
	WS_THICKFRAME = 0x00040000
	WS_BORDER = 0x00800000
	SWP_SHOWWINDOW = 0x0040
	WM_DESTROY = 0x0002
	WM_COMMAND = 0x0111
	WM_CLOSE = 0x0010
	CB_ADDSTRING = 0x0143
	CB_GETCURSEL = 0x0147
	CB_RESETCONTENT = 0x014B
	CB_SETCURSEL = 0x014E
	BM_GETCHECK = 0x00F0
	BM_SETCHECK = 0x00F1
	BST_CHECKED = 0x0001
	CBS_DROPDOWNLIST = 0x0003
	BS_PUSHBUTTON = 0x0000
	BS_AUTOCHECKBOX = 0x0003
	ES_LEFT = 0x0000
	WHITE_BRUSH = 0
	IDC_ARROW = 32512
	SM_CXSCREEN = 0
	SM_CYSCREEN = 1
	BN_CLICKED = 0
	GWL_STYLE = -16
	ID_WINDOW_COMBO = 1001
	ID_MONITOR_COMBO = 1002
	ID_WIDTH_EDIT = 1003
	ID_HEIGHT_EDIT = 1004
	ID_BORDERLESS_CHECK = 1005
	ID_REFRESH_BTN = 1006
	ID_RESIZE_BTN = 1007
	WM_APP_REFRESH_DONE = 0x8001
)

type (
	WNDCLASSEX struct {
		CbSize, Style uint32
		LpfnWndProc uintptr
		CbClsExtra, CbWndExtra int32
		HInstance, HIcon, HCursor, HbrBackground uintptr
		LpszMenuName, LpszClassName *uint16
		HIconSm uintptr
	}
	MSG struct {
		Hwnd uintptr
		Message uint32
		WParam, LParam uintptr
		Time uint32
		Pt struct{ X, Y int32 }
	}
	WindowInfo struct {
		Handle uintptr
		Title string
	}
	RECT struct{ Left, Top, Right, Bottom int32 }
	MONITORINFO struct {
		CbSize uint32
		RcMonitor, RcWork RECT
		DwFlags uint32
	}
	MonitorInfo struct{ Index, Width, Height int }
	Config      struct {
		Width      string `json:"width"`
		Height     string `json:"height"`
		Monitor    string `json:"monitor"`
		WindowTitle string `json:"window_title"`
		Borderless bool   `json:"borderless"`
	}
)

var (
	windows                                               []WindowInfo
	monitors                                              []MonitorInfo
	mainWindow, windowComboBox, monitorComboBox, widthEdit uintptr
	heightEdit, borderlessCheck, hInstance                uintptr
	configFile                                            string
	isRefreshing                                          bool
	windowsLock                                           sync.Mutex
	lastSavedMonitor, lastSavedWindow                     string
)

func initConfigPath() {
	dir, _ := os.UserConfigDir() // 默认返回 AppData\Roaming
	// 或者使用 os.Getenv("LOCALAPPDATA") 获取 AppData\Local
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

func saveConfig(width, height, monitor, windowTitle string, borderless bool) {
	if data, err := json.MarshalIndent(Config{width, height, monitor, windowTitle, borderless}, "", "  "); err == nil {
		os.WriteFile(configFile, data, 0644)
	}
}

func loadConfig() (c Config) {
	c = Config{"800", "600", "Monitor 1", "", true}
	if data, err := os.ReadFile(configFile); err == nil {
		json.Unmarshal(data, &c)
	}
	return
}

func showError(msg string) {
	procMessageBox.Call(mainWindow, u16(msg), u16("Error"), 0x30)
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
			res = append(res, MonitorInfo{idx + 1, int(mi.RcMonitor.Right - mi.RcMonitor.Left), int(mi.RcMonitor.Bottom - mi.RcMonitor.Top)})
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
		name := fmt.Sprintf("Monitor %d (%dx%d)", m.Index, m.Width, m.Height)
		if fmt.Sprintf("Monitor %d", m.Index) == lastSavedMonitor {
			sel = i
		}
		sendMsg(monitorComboBox, CB_ADDSTRING, 0, u16(name))
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
	if winIdx < 0 || winIdx >= len(local) || monIdx < 0 || errW != nil || errH != nil || w <= 0 || h <= 0 {
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

	sw, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	sh, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
	style, _, _ := procGetWindowLong.Call(hwnd, ^uintptr(15))
	if b {
		style &^= (WS_CAPTION | WS_THICKFRAME)
	} else {
		style |= (WS_CAPTION | WS_THICKFRAME)
	}
	procSetWindowLong.Call(hwnd, ^uintptr(15), style)
	if r, _, _ := procSetWindowPos.Call(hwnd, 0, uintptr((int(sw)-w)/2), uintptr((int(sh)-h)/2), uintptr(w), uintptr(h), SWP_SHOWWINDOW); r != 0 {
		lastSavedMonitor = fmt.Sprintf("Monitor %d", monIdx+1)
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
	procCreateWindowEx.Call(0, u16("STATIC"), u16("Target Window:"), WS_VISIBLE|WS_CHILD, 20, 20, 150, 30, mainWindow, 0, hInstance, 0)
	windowComboBox, _, _ = procCreateWindowEx.Call(0, u16("COMBOBOX"), 0, WS_VISIBLE|WS_CHILD|WS_TABSTOP|CBS_DROPDOWNLIST, 20, 55, 460, 250, mainWindow, ID_WINDOW_COMBO, hInstance, 0)
	procCreateWindowEx.Call(0, u16("STATIC"), u16("Target Monitor:"), WS_VISIBLE|WS_CHILD, 20, 105, 150, 30, mainWindow, 0, hInstance, 0)
	monitorComboBox, _, _ = procCreateWindowEx.Call(0, u16("COMBOBOX"), 0, WS_VISIBLE|WS_CHILD|WS_TABSTOP|CBS_DROPDOWNLIST, 20, 140, 460, 250, mainWindow, ID_MONITOR_COMBO, hInstance, 0)
	procCreateWindowEx.Call(0, u16("BUTTON"), u16("Refresh Lists"), WS_VISIBLE|WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON, 20, 190, 150, 40, mainWindow, ID_REFRESH_BTN, hInstance, 0)
	procCreateWindowEx.Call(0, u16("STATIC"), u16("Width:"), WS_VISIBLE|WS_CHILD, 20, 250, 80, 30, mainWindow, 0, hInstance, 0)
	widthEdit, _, _ = procCreateWindowEx.Call(0, u16("EDIT"), 0, WS_VISIBLE|WS_CHILD|WS_TABSTOP|WS_BORDER|ES_LEFT, 110, 248, 120, 30, mainWindow, ID_WIDTH_EDIT, hInstance, 0)
	procCreateWindowEx.Call(0, u16("STATIC"), u16("Height:"), WS_VISIBLE|WS_CHILD, 260, 250, 80, 30, mainWindow, 0, hInstance, 0)
	heightEdit, _, _ = procCreateWindowEx.Call(0, u16("EDIT"), 0, WS_VISIBLE|WS_CHILD|WS_TABSTOP|WS_BORDER|ES_LEFT, 350, 248, 120, 30, mainWindow, ID_HEIGHT_EDIT, hInstance, 0)
	borderlessCheck, _, _ = procCreateWindowEx.Call(0, u16("BUTTON"), u16("Borderless"), WS_VISIBLE|WS_CHILD|WS_TABSTOP|BS_AUTOCHECKBOX, 20, 300, 150, 30, mainWindow, ID_BORDERLESS_CHECK, hInstance, 0)
	procCreateWindowEx.Call(0, u16("BUTTON"), u16("Resize Window"), WS_VISIBLE|WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON, 20, 350, 180, 40, mainWindow, ID_RESIZE_BTN, hInstance, 0)
}

func main() {
	runtime.LockOSThread()
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
	createControls()
	cfg := loadConfig()
	lastSavedMonitor = cfg.Monitor
	lastSavedWindow = cfg.WindowTitle
	setText(widthEdit, cfg.Width)
	setText(heightEdit, cfg.Height)
	sendMsg(borderlessCheck, BM_SETCHECK, uintptr(map[bool]int{true: BST_CHECKED, false: 0}[cfg.Borderless]), 0)
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
