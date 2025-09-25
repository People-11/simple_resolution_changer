package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	// API functions
	procCreateWindowEx         = user32.NewProc("CreateWindowExW")
	procDefWindowProc          = user32.NewProc("DefWindowProcW")
	procRegisterClassEx        = user32.NewProc("RegisterClassExW")
	procGetMessage             = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessage        = user32.NewProc("DispatchMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procGetModuleHandle        = kernel32.NewProc("GetModuleHandleW")
	procLoadCursor             = user32.NewProc("LoadCursorW")
	procGetStockObject         = gdi32.NewProc("GetStockObject")
	procEnumWindows            = user32.NewProc("EnumWindows")
	procGetWindowText          = user32.NewProc("GetWindowTextW")
	procGetWindowTextLength    = user32.NewProc("GetWindowTextLengthW")
	procIsWindowVisible        = user32.NewProc("IsWindowVisible")
	procGetWindowLong          = user32.NewProc("GetWindowLongW")
	procSetWindowLong          = user32.NewProc("SetWindowLongW")
	procSetWindowPos           = user32.NewProc("SetWindowPos")
	procSetProcessDpiAwareness = shcore.NewProc("SetProcessDpiAwareness")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procSendMessage            = user32.NewProc("SendMessageW")
	procMessageBox             = user32.NewProc("MessageBoxW")
	procSetWindowText          = user32.NewProc("SetWindowTextW")
	procPostMessage            = user32.NewProc("PostMessageW")
	procEnumDisplayMonitors    = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfo         = user32.NewProc("GetMonitorInfoW")
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
	SM_CMONITORS        = 80
	SM_CXSCREEN         = 0
	SM_CYSCREEN         = 1
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
)

type WNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type WindowInfo struct {
	Handle uintptr
	Title  string
}

type RECT struct {
	Left, Top, Right, Bottom int32
}

type MONITORINFO struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
}

type MonitorInfo struct {
	Index, Width, Height int
}

type Config struct {
	Width      string `json:"width"`
	Height     string `json:"height"`
	Monitor    string `json:"monitor"`
	Borderless bool   `json:"borderless"`
}

var (
	windows           []WindowInfo
	monitors          []MonitorInfo
	mainWindow        uintptr
	windowComboBox    uintptr
	monitorComboBox   uintptr
	widthEdit         uintptr
	heightEdit        uintptr
	borderlessCheck   uintptr
	hInstance         uintptr
	configFile        = filepath.Join(os.TempDir(), "window_resizer_config.json")
	isRefreshing      bool
	windowsLock       sync.Mutex
)

func saveConfig(width, height, monitor string, borderless bool) {
	config := Config{width, height, monitor, borderless}
	if data, err := json.MarshalIndent(config, "", "  "); err == nil {
		os.WriteFile(configFile, data, 0644)
	}
}

func loadConfig() (width, height, monitor string, borderless bool) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return "800", "600", "Monitor 1", true
	}
	var config Config
	if json.Unmarshal(data, &config) != nil {
		return "800", "600", "Monitor 1", true
	}
	return config.Width, config.Height, config.Monitor, config.Borderless
}

func showError(message string) {
	procMessageBox.Call(mainWindow, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(message))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Error"))), 0x30)
}

func setText(hwnd uintptr, text string) {
	text16, _ := syscall.UTF16PtrFromString(text)
	procSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(text16)))
}

func getText(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLength.Call(hwnd)
	if length == 0 {
		return ""
	}
	buffer := make([]uint16, length+1)
	procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return syscall.UTF16ToString(buffer)
}

func getComboSelection(hwnd uintptr) int {
	index, _, _ := procSendMessage.Call(hwnd, CB_GETCURSEL, 0, 0)
	if index == ^uintptr(0) {
		return -1
	}
	return int(index)
}

func getCheckbox(hwnd uintptr) bool {
	state, _, _ := procSendMessage.Call(hwnd, BM_GETCHECK, 0, 0)
	return state == BST_CHECKED
}

func setCheckbox(hwnd uintptr, checked bool) {
	state := uintptr(0)
	if checked {
		state = BST_CHECKED
	}
	procSendMessage.Call(hwnd, BM_SETCHECK, state, 0)
}

func getMonitorInfo() []MonitorInfo {
	var monitorList []MonitorInfo
	index := 0

	enumFunc := func(hMonitor, hdcMonitor, lprcMonitor, dwData uintptr) uintptr {
		var mi MONITORINFO
		mi.CbSize = uint32(unsafe.Sizeof(mi))

		if ret, _, _ := procGetMonitorInfo.Call(hMonitor, uintptr(unsafe.Pointer(&mi))); ret != 0 {
			width := int(mi.RcMonitor.Right - mi.RcMonitor.Left)
			height := int(mi.RcMonitor.Bottom - mi.RcMonitor.Top)
			monitorList = append(monitorList, MonitorInfo{index + 1, width, height})
		}
		index++
		return 1
	}

	procEnumDisplayMonitors.Call(0, 0, syscall.NewCallback(enumFunc), 0)
	return monitorList
}

func updateUILists() {
	windowsLock.Lock()
	defer windowsLock.Unlock()

	procSendMessage.Call(windowComboBox, CB_RESETCONTENT, 0, 0)
	for _, win := range windows {
		title16, _ := syscall.UTF16PtrFromString(win.Title)
		procSendMessage.Call(windowComboBox, CB_ADDSTRING, 0, uintptr(unsafe.Pointer(title16)))
	}

	procSendMessage.Call(monitorComboBox, CB_RESETCONTENT, 0, 0)
	monitors = getMonitorInfo()
	
	for _, monitor := range monitors {
		name := fmt.Sprintf("Monitor %d (%dx%d)", monitor.Index, monitor.Width, monitor.Height)
		name16, _ := syscall.UTF16PtrFromString(name)
		procSendMessage.Call(monitorComboBox, CB_ADDSTRING, 0, uintptr(unsafe.Pointer(name16)))
	}
	if len(monitors) > 0 {
		procSendMessage.Call(monitorComboBox, CB_SETCURSEL, 0, 0)
	}
}

func refreshLists() {
	if isRefreshing {
		return
	}
	isRefreshing = true
	defer func() { isRefreshing = false }()

	var handles []uintptr
	enumFunc := func(hwnd uintptr, lParam uintptr) uintptr {
		handles = append(handles, hwnd)
		return 1
	}
	procEnumWindows.Call(syscall.NewCallback(enumFunc), 0)

	localWindows := []WindowInfo{}
	for _, hwnd := range handles {
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
			continue
		}
		length, _, _ := procGetWindowTextLength.Call(hwnd)
		if length == 0 || length > 256 {
			continue
		}
		buffer := make([]uint16, length+1)
		procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
		title := syscall.UTF16ToString(buffer)

		if len(title) > 0 && title != "Window Resizer" {
			localWindows = append(localWindows, WindowInfo{Handle: hwnd, Title: title})
		}
	}

	windowsLock.Lock()
	windows = localWindows
	windowsLock.Unlock()

	procPostMessage.Call(mainWindow, WM_APP_REFRESH_DONE, 0, 0)
}

func performResize() {
	windowsLock.Lock()
	localWindows := make([]WindowInfo, len(windows))
	copy(localWindows, windows)
	windowsLock.Unlock()

	windowIndex := getComboSelection(windowComboBox)
	if windowIndex < 0 || windowIndex >= len(localWindows) {
		showError("Please select a window.")
		return
	}

	monitorIndex := getComboSelection(monitorComboBox)
	if monitorIndex < 0 {
		showError("Please select a monitor.")
		return
	}

	widthStr := getText(widthEdit)
	heightStr := getText(heightEdit)

	width, errW := strconv.Atoi(widthStr)
	height, errH := strconv.Atoi(heightStr)
	if errW != nil || errH != nil || width <= 0 || height <= 0 {
		showError("Invalid width or height.")
		return
	}

	borderless := getCheckbox(borderlessCheck)
	hwnd := localWindows[windowIndex].Handle

	screenWidth, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	screenHeight, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
	centerX := (int(screenWidth) - width) / 2
	centerY := (int(screenHeight) - height) / 2

	style, _, _ := procGetWindowLong.Call(hwnd, ^uintptr(15))
	if borderless {
		style &^= (WS_CAPTION | WS_THICKFRAME)
	} else {
		style |= (WS_CAPTION | WS_THICKFRAME)
	}
	procSetWindowLong.Call(hwnd, ^uintptr(15), style)

	ret, _, _ := procSetWindowPos.Call(hwnd, 0, uintptr(centerX), uintptr(centerY), uintptr(width), uintptr(height), SWP_SHOWWINDOW)
	if ret != 0 {
		monitorName := fmt.Sprintf("Monitor %d", monitorIndex+1)
		go saveConfig(widthStr, heightStr, monitorName, borderless)
	} else {
		showError("Failed to resize window.")
	}
}

func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_DESTROY, WM_CLOSE:
		procPostQuitMessage.Call(0)
		return 0
	case WM_APP_REFRESH_DONE:
		updateUILists()
		return 0
	case WM_COMMAND:
		controlID := wParam & 0xFFFF
		notificationCode := (wParam >> 16) & 0xFFFF
		if notificationCode == BN_CLICKED {
			switch controlID {
			case ID_REFRESH_BTN:
				go refreshLists()
			case ID_RESIZE_BTN:
				performResize()
			}
		}
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return ret
}

func createControls() {
	procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("STATIC"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Target Window:"))),
		WS_VISIBLE|WS_CHILD, 20, 20, 150, 30, mainWindow, 0, hInstance, 0)
	windowComboBox, _, _ = procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("COMBOBOX"))),
		0, WS_VISIBLE|WS_CHILD|WS_TABSTOP|CBS_DROPDOWNLIST,
		20, 55, 460, 250, mainWindow, ID_WINDOW_COMBO, hInstance, 0)

	procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("STATIC"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Target Monitor:"))),
		WS_VISIBLE|WS_CHILD, 20, 105, 150, 30, mainWindow, 0, hInstance, 0)
	monitorComboBox, _, _ = procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("COMBOBOX"))),
		0, WS_VISIBLE|WS_CHILD|WS_TABSTOP|CBS_DROPDOWNLIST,
		20, 140, 460, 250, mainWindow, ID_MONITOR_COMBO, hInstance, 0)

	procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("BUTTON"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Refresh Lists"))),
		WS_VISIBLE|WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON,
		20, 190, 150, 40, mainWindow, ID_REFRESH_BTN, hInstance, 0)

	procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("STATIC"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Width:"))),
		WS_VISIBLE|WS_CHILD, 20, 250, 80, 30, mainWindow, 0, hInstance, 0)
	widthEdit, _, _ = procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("EDIT"))),
		0, WS_VISIBLE|WS_CHILD|WS_TABSTOP|WS_BORDER|ES_LEFT,
		110, 248, 120, 30, mainWindow, ID_WIDTH_EDIT, hInstance, 0)

	procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("STATIC"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Height:"))),
		WS_VISIBLE|WS_CHILD, 260, 250, 80, 30, mainWindow, 0, hInstance, 0)
	heightEdit, _, _ = procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("EDIT"))),
		0, WS_VISIBLE|WS_CHILD|WS_TABSTOP|WS_BORDER|ES_LEFT,
		350, 248, 120, 30, mainWindow, ID_HEIGHT_EDIT, hInstance, 0)

	borderlessCheck, _, _ = procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("BUTTON"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Borderless"))),
		WS_VISIBLE|WS_CHILD|WS_TABSTOP|BS_AUTOCHECKBOX,
		20, 300, 150, 30, mainWindow, ID_BORDERLESS_CHECK, hInstance, 0)

	procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("BUTTON"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Resize Window"))),
		WS_VISIBLE|WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON,
		20, 350, 180, 40, mainWindow, ID_RESIZE_BTN, hInstance, 0)
}

func main() {
	runtime.LockOSThread()

	procSetProcessDpiAwareness.Call(uintptr(1))

	hInstance, _, _ = procGetModuleHandle.Call(0)

	className := "WindowResizerClass"
	className16, _ := syscall.UTF16PtrFromString(className)
	var wc WNDCLASSEX
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = syscall.NewCallback(wndProc)
	wc.HInstance = hInstance
	wc.HCursor, _, _ = procLoadCursor.Call(0, IDC_ARROW)
	wc.HbrBackground, _, _ = procGetStockObject.Call(WHITE_BRUSH)
	wc.LpszClassName = className16
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))

	title16, _ := syscall.UTF16PtrFromString("Window Resizer")
	mainWindow, _, _ = procCreateWindowEx.Call(0,
		uintptr(unsafe.Pointer(className16)),
		uintptr(unsafe.Pointer(title16)),
		WS_OVERLAPPEDWINDOW|WS_VISIBLE,
		200, 150, 520, 460,
		0, 0, hInstance, 0)

	if mainWindow == 0 {
		return
	}

	createControls()

	width, height, _, borderless := loadConfig()
	setText(widthEdit, width)
	setText(heightEdit, height)
	setCheckbox(borderlessCheck, borderless)

	go refreshLists()

	var msg MSG
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}