package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/darkhz/invidtui/lib"
	"github.com/darkhz/tview"
	"github.com/gdamore/tcell/v2"
)

var (
	// App contains the application.
	App *tview.Application

	// UIFlex contains the arranged UI elements.
	UIFlex *tview.Flex

	// VPage holds the ResultsList and other list views
	// like the playlist view for example.
	VPage *tview.Pages

	// MPage holds the entire UI Flexbox. This is needed to
	// align and display popups properly.
	MPage *tview.Pages

	mainStyle tcell.Style
	auxStyle  tcell.Style

	appSuspend  bool
	bannerShown bool
	detectClose chan struct{}
)

const banner = `
   (_)____  _   __ (_)____/ // /_ __  __ (_)
  / // __ \| | / // // __  // __// / / // /
 / // / / /| |/ // // /_/ // /_ / /_/ // /
/_//_/ /_/ |___//_/ \__,_/ \__/ \__,_//_/
`

// SetupUI sets up the UI and starts the application.
func SetupUI() error {
	setupPrimitives()

	mainStyle = tcell.Style{}.
		Foreground(tcell.ColorBlue).
		Background(tcell.ColorWhite).
		Attributes(tcell.AttrBold)

	auxStyle = tcell.Style{}.
		Attributes(tcell.AttrBold)

	MPage = tview.NewPages()
	MPage.AddPage("ui", UIFlex, true, true)

	App = tview.NewApplication()
	App.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlC:
			return nil

		case tcell.KeyCtrlD:
			if pg, _ := VPage.GetFrontPage(); pg != "dashboard" {
				go ShowDashboard()
			}

		case tcell.KeyCtrlZ:
			appSuspend = true

		case tcell.KeyCtrlX:
			lib.VideoCancel()
			lib.SearchCancel()
			lib.PlaylistCancel()
			lib.ClientSendCancel()
			closeCommentView()
			InfoMessage("Loading canceled", false)
		}

		switch event.Rune() {
		case 'q':
			if _, ok := App.GetFocus().(*tview.InputField); !ok {
				confirmQuit()
				return nil
			}
		}

		return event
	})

	App.SetAfterDrawFunc(func(t tcell.Screen) {
		width, height := t.Size()

		suspendUI(t)
		resizePlayer(width)
		resizeListEntries(width)
		resizePopup(width, height)
	})

	msg := "Instance '" + lib.GetClient().SelectedInstance() + "' selected. "
	msg += "Press / to search."
	InfoMessage(msg, true)

	detectClose = make(chan struct{})
	go detectMPVClose()

	parseSearchCmd()
	parsePlayParams()

	_, focusedItem := VPage.GetFrontPage()
	if err := App.SetRoot(MPage, true).SetFocus(focusedItem).Run(); err != nil {
		panic(err)
	}

	return nil
}

// StopUI stops the application.
func StopUI(closeInstances bool) {
	close(detectClose)

	StopPlayer(closeInstances)
	App.Stop()
}

// suspendUI suspends the application.
func suspendUI(t tcell.Screen) {
	if !appSuspend {
		return
	}

	lib.SuspendApp(t)

	appSuspend = false
}

// setupPrimitives sets up the display elements and positions
// each element appropriately.
func setupPrimitives() {
	SetupList()
	SetupInputBox()
	SetupMessageBox()
	SetupStatus()
	SetupPlayer()
	SetupFileBrowser()
	SetupPlaylist()

	VPage = tview.NewPages()
	VPage.AddPage("banner", showBanner(), true, true)
	VPage.AddPage("search", ResultsFlex, true, false)

	box := tview.NewBox().
		SetBackgroundColor(tcell.ColorDefault)

	UIFlex = tview.NewFlex().
		AddItem(VPage, 0, 10, false).
		AddItem(box, 1, 0, false).
		AddItem(Status, 1, 0, false).
		SetDirection(tview.FlexRow)

	UIFlex.SetBackgroundColor(tcell.ColorDefault)
}

// showBanner displays the banner on the screen.
func showBanner() tview.Primitive {
	lines := strings.Split(banner, "\n")
	bannerWidth := 0
	bannerHeight := len(lines)
	for _, line := range lines {
		if len(line) > bannerWidth {
			bannerWidth = len(line)
		}
	}
	bannerBox := tview.NewTextView()
	bannerBox.SetDynamicColors(true)
	bannerBox.SetBackgroundColor(tcell.ColorDefault)
	bannerBox.SetText("[::b]" + banner)

	box := tview.NewBox().
		SetBackgroundColor(tcell.ColorDefault)

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(box, 0, 7, false).
		AddItem(tview.NewFlex().
			AddItem(box, 0, 1, false).
			AddItem(bannerBox, bannerWidth, 1, true).
			AddItem(box, 0, 1, false), bannerHeight, 1, true).
		AddItem(box, 0, 7, false)
	flex.SetBackgroundColor(tcell.ColorDefault)
	flex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		capturePlayerEvent(event)

		switch event.Rune() {
		case '/':
			searchText(false)

		case 'i', 'u', 'U':
			if event.Modifiers() == tcell.ModAlt {
				ResultsList.InputHandler()(event, nil)
			}
		}

		return event
	})

	bannerShown = true

	return flex
}

// confirmQuit shows a confirmation message before exiting.
func confirmQuit() {
	p := App.GetFocus()
	pg, _ := Status.GetFrontPage()
	label, max, dofunc, chgfunc, infunc := GetInputProps()

	qfocus := func() {
		SetInput(label, max, dofunc, infunc, chgfunc)

		Status.SwitchToPage(pg)
		App.SetFocus(p)
	}

	qfunc := func(text string) {
		if text == "y" {
			StopUI(false)
		} else {
			qfocus()
		}
	}

	ifunc := func(e *tcell.EventKey) *tcell.EventKey {
		switch e.Key() {
		case tcell.KeyEnter:
			qfunc(InputBox.GetText())

		case tcell.KeyEscape:
			qfocus()
		}

		return e
	}

	SetInput("Quit? (y/n)", 1, qfunc, ifunc)
}

// detectMPVClose detects if MPV has exited unexpectedly,
// and stops the application.
func detectMPVClose() {
	lib.GetMPV().WaitUntilClosed()

	select {
	case _, ok := <-detectClose:
		if !ok {
			return
		}

	default:
	}

	StopUI(true)

	if socket, err := lib.ConfigPath("socket"); err == nil {
		os.Remove(socket)
	}

	fmt.Printf("\rMPV has exited")
}
