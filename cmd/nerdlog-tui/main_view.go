package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dimonomid/nerdlog/clhistory"
	"github.com/dimonomid/nerdlog/core"
	"github.com/gdamore/tcell/v2"
	"github.com/juju/errors"
	"github.com/rivo/tview"
)

const logsTableTimeLayout = "Jan02 15:04:05.000"

const (
	pageNameMessage         = "message"
	pageNameEditQueryParams = "message"
)

const (
	// rowIdxLoadOlder is the index of the row acting as a button to load more (older) logs
	rowIdxLoadOlder = 1
)

const histogramBinSize = 60 // 1 minute

type MainViewParams struct {
	App *tview.Application

	// OnLogQuery is called by MainView whenever the user submits a query to get
	// logs.
	OnLogQuery OnLogQueryCallback

	OnHostsFilterChange OnHostsFilterChange

	// TODO: support command history
	OnCmd OnCmdCallback

	CmdHistory *clhistory.CLHistory
}

type MainView struct {
	params    MainViewParams
	rootPages *tview.Pages
	logsTable *tview.Table

	queryInput *tview.InputField
	cmdInput   *tview.InputField

	topFlex      *tview.Flex
	queryEditBtn *tview.Button
	timeLabel    *tview.TextView

	queryEditView *QueryEditView

	// overlayMsgView is nil if there's no overlay msg.
	overlayMsgView *MessageView
	overlayText    string
	overlaySpinner rune

	// focusedBeforeCmd is a primitive which was focused before cmdInput was
	// focused. Once the user is done editing command, focusedBeforeCmd
	// normally resumes focus.
	focusedBeforeCmd tview.Primitive

	histogram *Histogram

	statusLineLeft  *tview.TextView
	statusLineRight *tview.TextView

	hostsFilter string

	// from, to represent the selected time range
	from, to TimeOrDur

	// query is the effective search query
	query string

	// actualFrom, actualTo represent the actual time range resolved from from
	// and to, and they both can't be zero.
	actualFrom, actualTo time.Time

	// When doQueryOnceConnected is true, it means that whenever we get a new
	// status update (ApplyHMState gets called), if Connected is true there,
	// we'll call doQuery().
	doQueryOnceConnected bool

	curHMState *core.HostsManagerState
	curLogResp *core.LogRespTotal
	// statsFrom and statsTo represent the first and last element present
	// in curLogResp.MinuteStats. Note that this range might be smaller than
	// (from, to), because for some minute stats might be missing. statsFrom
	// and statsTo are only useful for cases when from and/or to are zero (meaning,
	// time range isn't limited)
	statsFrom, statsTo time.Time

	//marketViewsByID map[common.MarketID]*MarketView
	//marketDescrByID map[common.MarketID]MarketDescr

	modalsFocusStack []tview.Primitive
}

type OnLogQueryCallback func(params core.QueryLogsParams)
type OnHostsFilterChange func(hostsFilter string) error
type OnCmdCallback func(cmd string)

var (
	queryInputStaleMatch = tcell.Style{}.
				Background(tcell.ColorBlue).
				Foreground(tcell.ColorWhite).
				Bold(true)

	queryInputStaleMismatch = tcell.Style{}.
				Background(tcell.ColorDarkRed).
				Foreground(tcell.ColorWhite).
				Bold(true)
)

func NewMainView(params *MainViewParams) *MainView {
	mv := &MainView{
		params: *params,
	}

	mv.rootPages = tview.NewPages()

	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow)

	mv.queryInput = tview.NewInputField()
	mv.queryInput.SetDoneFunc(func(key tcell.Key) {
		switch key {

		case tcell.KeyEnter:
			mv.setQuery(mv.queryInput.GetText())
			mv.bumpTimeRange(false)
			mv.doQuery()
			mv.queryInputApplyStyle()

		case tcell.KeyEsc:
			//if mv.queryInput.GetText() != mv.query {
			//mv.queryInput.SetText(mv.query)
			//mv.queryInputApplyStyle()
			//}
			mv.params.App.SetFocus(mv.logsTable)

		case tcell.KeyTab:
			mv.params.App.SetFocus(mv.queryEditBtn)

		case tcell.KeyBacktab:
			mv.params.App.SetFocus(mv.logsTable)
		}
	})

	mv.queryInput.SetChangedFunc(func(text string) {
		mv.queryInputApplyStyle()
	})

	mv.queryInputApplyStyle()

	mv.queryEditBtn = tview.NewButton("Edit")
	mv.queryEditBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			mv.params.App.SetFocus(mv.histogram)
		case tcell.KeyBacktab:
			mv.params.App.SetFocus(mv.queryInput)
			return nil

		case tcell.KeyEsc:
			mv.params.App.SetFocus(mv.logsTable)

		case tcell.KeyRune:
			switch event.Rune() {
			case ':':
				mv.focusCmdline()
			}
		}

		return event
	})
	mv.queryEditBtn.SetSelectedFunc(func() {
		mv.queryEditView.Show(mv.getQueryFull())
	})

	queryLabel := tview.NewTextView()
	queryLabel.SetScrollable(false).SetText("Query:")

	mv.timeLabel = tview.NewTextView()
	mv.timeLabel.SetScrollable(false)

	mv.topFlex = tview.NewFlex().SetDirection(tview.FlexColumn)
	mv.topFlex.
		AddItem(queryLabel, 6, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(mv.queryInput, 0, 1, true).
		AddItem(nil, 1, 0, false).
		AddItem(mv.timeLabel, 1, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(mv.queryEditBtn, 6, 0, false)

	mainFlex.AddItem(mv.topFlex, 1, 0, true)

	mv.histogram = NewHistogram()
	mv.histogram.SetBinSize(histogramBinSize) // 1 minute
	mv.histogram.SetXFormatter(func(v int) string {
		t := time.Unix(int64(v), 0).UTC()
		return t.Format("15:04")
	})
	mv.histogram.SetXMarker(func(from, to int, numChars int) []int {
		// TODO proper impl

		diff := to - from

		var step int
		if diff <= 10*histogramBinSize {
			step = histogramBinSize
		} else {
			step = diff / 6
			if step == 0 {
				return nil
			}

			// Snap to 1m grid: make sure our marks will be on minute boundaries
			tmp := (step + histogramBinSize/2) / histogramBinSize
			step = tmp * histogramBinSize
		}

		ret := []int{}
		for i := from; i <= to; i += step {
			ret = append(ret, i)
		}

		return ret
	})
	mv.histogram.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			mv.params.App.SetFocus(mv.logsTable)
		case tcell.KeyBacktab:
			mv.params.App.SetFocus(mv.queryEditBtn)
			return nil

		case tcell.KeyEsc:
			if !mv.histogram.IsSelectionActive() {
				mv.params.App.SetFocus(mv.logsTable)
				return nil
			}

		case tcell.KeyRune:
			switch event.Rune() {
			case ':':
				mv.focusCmdline()
			}
		}

		return event
	})
	mv.histogram.SetSelectedFunc(func(from, to int) {
		fromTime := TimeOrDur{
			Time: time.Unix(int64(from), 0).UTC(),
		}

		toTime := TimeOrDur{
			Time: time.Unix(int64(to), 0).UTC(),
		}

		mv.setTimeRange(fromTime, toTime)
		mv.doQuery()
	})

	mainFlex.AddItem(mv.histogram, 6, 0, false)

	mv.logsTable = tview.NewTable()
	mv.updateTableHeader(nil)

	//mv.logsTable.SetEvaluateAllRows(true)
	mv.logsTable.SetFocusFunc(func() {
		mv.logsTable.SetSelectable(true, false)
	})
	mv.logsTable.SetBlurFunc(func() {
		mv.logsTable.SetSelectable(false, false)
	})

	mv.logsTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()

		switch key {
		case tcell.KeyCtrlD:
			// TODO: ideally we'd want to only go half a page down, but for now just
			// return Ctrl+F which will go the full page down
			return tcell.NewEventKey(tcell.KeyCtrlF, 0, tcell.ModNone)
		case tcell.KeyCtrlU:
			// TODO: ideally we'd want to only go half a page up, but for now just
			// return Ctrl+B which will go the full page up
			return tcell.NewEventKey(tcell.KeyCtrlB, 0, tcell.ModNone)

		case tcell.KeyRune:
			switch event.Rune() {
			case ':':
				mv.focusCmdline()
			}
		}

		return event
	})

	// TODO: once tableview fixed, use SetFixed(1, 1)
	// (there's an issue with going to the very top using "g")
	mv.logsTable.SetFixed(1, 1)
	mv.logsTable.Select(0, 0).SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			//mv.logsTable.SetSelectable(true, true)
		}
		if key == tcell.KeyTab {
			mv.params.App.SetFocus(mv.queryInput)
		}
		if key == tcell.KeyBacktab {
			mv.params.App.SetFocus(mv.histogram)
		}
	}).SetSelectedFunc(func(row int, column int) {
		if row == rowIdxLoadOlder {
			// Request to load more (older) logs

			// Do the query to core
			mv.params.OnLogQuery(core.QueryLogsParams{
				From:  mv.actualFrom,
				To:    mv.actualTo,
				Query: mv.query,

				LoadEarlier: true,
			})

			// Update the cell text
			mv.logsTable.SetCell(
				rowIdxLoadOlder, 0,
				newTableCellButton("... loading ..."),
			)
			return
		}

		// "Click" on a data cell: show original message

		timeCell := mv.logsTable.GetCell(row, 0)
		msg := timeCell.GetReference().(core.LogMsg)

		lnOffsetUp := 1000   // How many surrounding lines to show, up
		lnOffsetDown := 1000 // How many surrounding lines to show, down
		lnBegin := msg.LogLinenumber - lnOffsetUp
		if lnBegin <= 0 {
			lnOffsetUp += lnBegin - 1
			lnBegin = 1
		}

		s := fmt.Sprintf(
			"ssh -t %s 'vim +\"set ft=messages\" +%d <(tail -n +%d %s | head -n %d)'\n\n%s",
			msg.Context["source"], lnOffsetUp+1, lnBegin, msg.LogFilename, lnOffsetUp+lnOffsetDown,
			msg.OrigLine,
		)

		mv.showMessagebox("msg", "Message", s, &MessageboxParams{
			Width:  120,
			Height: 20,
		})
	}).SetSelectionChangedFunc(func(row, column int) {
		mv.bumpStatusLineRight()
	})

	/*

		lorem := strings.Split("Lorem iipsum-[:red:b]ipsum[:-:-]-ipsum-[::b]ipsum[::-]-ipsum-ipsum-ipsum-ipsum-ipsum-ipsum-ipsum-ipsum-ipsum-ipsum-ipsum-ipsum-ipsum-psum- dolor sit amet, consetetur sadipscing elitr, sed diam nonumy eirmod tempor invidunt ut labore et dolore magna aliquyam erat, sed diam voluptua. At vero eos et accusam et justo duo dolores et ea rebum. Stet clita kasd gubergren, no sea takimata sanctus est Lorem ipsum dolor sit amet. Lorem ipsum dolor sit amet, consetetur sadipscing elitr, sed diam nonumy eirmod tempor invidunt ut labore et dolore magna aliquyam erat, sed diam voluptua. At vero eos et accusam et justo duo dolores et ea rebum. Stet clita kasd gubergren, no sea takimata sanctus est Lorem ipsum dolor sit amet.", " ")
		cols, rows := 10, 400
		word := 0
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				color := tcell.ColorWhite
				if c < 1 || r < 1 {
					color = tcell.ColorYellow
				}
				mv.logsTable.SetCell(r, c,
					tview.NewTableCell(lorem[word]).
						SetTextColor(color).
						SetAlign(tview.AlignLeft))
				word = (word + 1) % len(lorem)
			}
		}
	*/

	mainFlex.AddItem(mv.logsTable, 0, 1, false)

	mv.statusLineLeft = tview.NewTextView()
	mv.statusLineLeft.SetScrollable(false).SetDynamicColors(true)

	mv.statusLineRight = tview.NewTextView()
	mv.statusLineRight.SetTextAlign(tview.AlignRight).SetScrollable(false).SetDynamicColors(true)

	statusLineFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	statusLineFlex.
		AddItem(mv.statusLineLeft, 0, 1, false).
		AddItem(nil, 1, 0, false).
		AddItem(mv.statusLineRight, 30, 0, true)

	mainFlex.AddItem(statusLineFlex, 1, 0, false)

	mv.cmdInput = tview.NewInputField()
	mv.cmdInput.SetChangedFunc(func(text string) {
		if text == "" {
			mv.params.App.SetFocus(mv.focusedBeforeCmd)
		}
	})

	mv.cmdInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		cmd := mv.cmdInput.GetText()
		// Remove the ":" prefix
		cmd = cmd[1:]

		switch event.Key() {
		case tcell.KeyCtrlP:
			item := mv.params.CmdHistory.Prev(cmd)
			mv.cmdInput.SetText(":" + item.Str)
			return nil

		case tcell.KeyCtrlN:
			item := mv.params.CmdHistory.Next(cmd)
			mv.cmdInput.SetText(":" + item.Str)
			return nil
		}

		mv.params.CmdHistory.Reset()

		return event
	})

	mv.cmdInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			cmd := mv.cmdInput.GetText()

			// Remove the ":" prefix
			cmd = cmd[1:]

			if cmd != "" {
				mv.params.OnCmd(cmd)
			} else {
				// Similarly to zsh, make it so that an empty command causes history to
				// be reloaded.  TODO: maybe make it so that we reload it after any
				// command, actually.
				mv.params.CmdHistory.Load()
			}

		case tcell.KeyEsc:
		// Gonna just stop editing it
		default:
			// Ignore it
			return
		}

		mv.cmdInput.SetText("")
		mv.params.CmdHistory.Reset()
	})

	mainFlex.AddItem(mv.cmdInput, 1, 0, false)

	mv.queryEditView = NewQueryEditView(mv, &QueryEditViewParams{
		DoneFunc: mv.applyQueryEditData,
	})

	mv.rootPages.AddPage("mainFlex", mainFlex, true, true)

	// Set default time range, just to have anything there.
	from, to := TimeOrDur{Dur: -1 * time.Hour}, TimeOrDur{}
	mv.setTimeRange(from, to)

	go mv.run()

	return mv
}

func (mv *MainView) focusCmdline() {
	mv.cmdInput.SetText(":")
	mv.focusedBeforeCmd = mv.params.App.GetFocus()
	mv.params.App.SetFocus(mv.cmdInput)
}

func (mv *MainView) run() {
	ticker := time.NewTicker(250 * time.Millisecond)

	for {
		select {
		case <-ticker.C:
			needDraw := false
			mv.params.App.QueueUpdate(func() {
				needDraw = mv.tick()
			})

			if needDraw {
				mv.params.App.Draw()
			}
		}
	}
}

func (mv *MainView) tick() (needDraw bool) {
	if mv.overlayMsgView != nil {
		switch mv.overlaySpinner {
		case '-':
			mv.overlaySpinner = '\\'
		case '\\':
			mv.overlaySpinner = '|'
		case '|':
			mv.overlaySpinner = '/'
		case '/':
			mv.overlaySpinner = '-'
		}

		mv.bumpOverlay()

		needDraw = true
	}

	return needDraw
}

func (mv *MainView) bumpOverlay() {
	mv.overlayMsgView.textView.SetText(string(mv.overlaySpinner) + " " + mv.overlayText)
}

func (mv *MainView) queryInputApplyStyle() {
	style := queryInputStaleMatch
	if mv.queryInput.GetText() != mv.query {
		style = queryInputStaleMismatch
	}

	mv.queryInput.SetFieldStyle(style)
}

func (mv *MainView) applyQueryEditData(data QueryFull) error {
	ftr, err := ParseFromToRange(data.Time)
	if err != nil {
		return errors.Annotatef(err, "time")
	}

	mv.setQuery(data.Query)
	mv.setTimeRange(ftr.From, ftr.To)

	// Before we call OnHostsFilterChange, set this doQueryOnceConnected
	// flag, so that once we receive a status update (which can happen any
	// moment after OnHostsFilterChange is called), and if Connected is true
	// there, we'll do the query.
	mv.doQueryOnceConnected = true

	err = mv.params.OnHostsFilterChange(data.HostsFilter)
	if err != nil {
		return errors.Annotate(err, "hosts")
	}

	mv.setHostsFilter(data.HostsFilter)

	mv.bumpStatusLineLeft()

	mv.queryInputApplyStyle()

	return nil
}

func (mv *MainView) GetUIPrimitive() tview.Primitive {
	return mv.rootPages
}

func (mv *MainView) applyHMState(hmState *core.HostsManagerState) {
	mv.curHMState = hmState
	var overlayMsg string

	if !mv.curHMState.Connected && !mv.curHMState.NoMatchingHosts {
		overlayMsg = "Connecting to hosts..."
	} else if mv.curHMState.Busy {
		overlayMsg = "Updating search results..."
	}

	if overlayMsg != "" {
		// Need to show or update overlay message.
		if mv.overlayMsgView == nil {
			mv.overlayMsgView = mv.showMessagebox(
				"overlay_msg", "", "", &MessageboxParams{
					Buttons: []string{},
					NoFocus: true,
					Width:   40,
					Height:  6,

					BackgroundColor: tcell.ColorDarkBlue,
				},
			)

			mv.overlaySpinner = '-'
		}

		mv.overlayText = overlayMsg
		mv.bumpOverlay()
	} else if mv.overlayMsgView != nil {
		// Need to hide overlay message.

		// TODO: using pageNameMessage here directly is too hacky
		mv.hideModal(pageNameMessage+"overlay_msg", false)
		mv.overlayMsgView = nil
		mv.overlayText = ""
	}

	mv.bumpStatusLineLeft()

	if mv.curHMState.Connected && mv.doQueryOnceConnected {
		mv.doQuery()
		mv.doQueryOnceConnected = false
	}
}

func getStatuslineNumStr(icon string, num int, color string) string {
	mod := "-"
	if num > 0 {
		mod = "b"
	}

	return fmt.Sprintf("[%s:-:%s]%s %.2d[-:-:-]", color, mod, icon, num)
}

func (mv *MainView) updateTableHeader(msgs []core.LogMsg) (colNames []string) {
	whitelisted := map[string]struct{}{
		"redacted_id_int":     {},
		"redacted_symbol_str": {},
		"level_name":            {},
		"namespace":             {},
		"series_ids_string":     {},
		"series_slug_str":       {},
		"series_type_str":       {},
	}

	tagNamesSet := map[string]struct{}{}
	for _, msg := range msgs {
		for name := range msg.Context {
			if _, ok := whitelisted[name]; !ok {
				continue
			}

			tagNamesSet[name] = struct{}{}
		}
	}

	delete(tagNamesSet, "source")
	delete(tagNamesSet, "level_name")

	tagNames := make([]string, 0, len(tagNamesSet))
	for name := range tagNamesSet {
		tagNames = append(tagNames, name)
	}

	sort.Strings(tagNames)

	colNames = append([]string{"time", "message", "source", "level_name"}, tagNames...)

	// Add header
	for i, colName := range colNames {
		mv.logsTable.SetCell(
			0, i,
			newTableCellHeader(colName),
		)
	}

	return colNames
}

func (mv *MainView) applyLogs(resp *core.LogRespTotal) {
	mv.curLogResp = resp

	histogramData := make(map[int]int, len(resp.MinuteStats))
	for k, v := range resp.MinuteStats {
		histogramData[int(k)] = v.NumMsgs
	}

	mv.histogram.SetData(histogramData)

	// TODO: perhaps optimize it, instead of clearing and repopulating whole table
	oldNumRows := mv.logsTable.GetRowCount()
	selectedRow, _ := mv.logsTable.GetSelection()
	offsetRow, offsetCol := mv.logsTable.GetOffset()
	mv.logsTable.Clear()

	colNames := mv.updateTableHeader(resp.Logs)

	mv.logsTable.SetCell(
		rowIdxLoadOlder, 0,
		newTableCellButton("< MOAR ! >"),
	)

	// Add all available logs
	for i, rowIdx := 0, 2; i < len(resp.Logs); i, rowIdx = i+1, rowIdx+1 {
		msg := resp.Logs[i]

		msgColor := tcell.ColorWhite
		switch msg.Context["level_name"] {
		case "warn":
			msgColor = tcell.ColorYellow
		case "error":
			msgColor = tcell.ColorPink
		}

		timeStr := msg.Time.Format(logsTableTimeLayout)
		if msg.DecreasedTimestamp {
			timeStr = ""
		}

		mv.logsTable.SetCell(
			rowIdx, 0,
			newTableCellLogmsg(timeStr).SetTextColor(tcell.ColorLightBlue),
		)

		mv.logsTable.SetCell(
			rowIdx, 1,
			newTableCellLogmsg(msg.Msg).SetTextColor(msgColor),
		)

		for i, colName := range colNames[2:] {
			mv.logsTable.SetCell(
				rowIdx, 2+i,
				newTableCellLogmsg(msg.Context[colName]).SetTextColor(msgColor),
			)
		}

		mv.logsTable.GetCell(rowIdx, 0).SetReference(msg)

		//msg.
	}

	if !resp.LoadedEarlier {
		// Replaced all logs
		mv.logsTable.Select(len(resp.Logs)+1, 0)
		mv.logsTable.ScrollToEnd()
		mv.bumpTimeRange(true)
	} else {
		// Loaded more (earlier) logs
		numNewRows := mv.logsTable.GetRowCount() - oldNumRows
		mv.logsTable.SetOffset(offsetRow+numNewRows, offsetCol)
		mv.logsTable.Select(selectedRow+numNewRows, 0)
	}

	mv.bumpStatusLineRight()
}

func (mv *MainView) bumpStatusLineLeft() {
	sb := strings.Builder{}

	hmState := mv.curHMState
	if hmState == nil {
		// We haven't received a single HMState update, so just use the zero value.
		hmState = &core.HostsManagerState{}
	}

	if !hmState.Connected && !hmState.NoMatchingHosts {
		sb.WriteString("connecting ")
	} else if hmState.Busy {
		sb.WriteString("busy ")
	} else {
		sb.WriteString("idle ")
	}

	numIdle := len(hmState.HostsByState[core.HostAgentStateConnectedIdle])
	numBusy := len(hmState.HostsByState[core.HostAgentStateConnectedBusy])
	numOther := hmState.NumHosts - numIdle - numBusy

	sb.WriteString(getStatuslineNumStr("🖳", numIdle, "green"))
	sb.WriteString(" ")
	sb.WriteString(getStatuslineNumStr("🖳", numBusy, "orange"))
	sb.WriteString(" ")
	sb.WriteString(getStatuslineNumStr("🖳", numOther, "red"))
	sb.WriteString(" ")
	sb.WriteString(getStatuslineNumStr("🖳", hmState.NumUnused, "gray"))

	sb.WriteString(" | ")
	sb.WriteString(mv.hostsFilter)

	mv.statusLineLeft.SetText(sb.String())
}

func (mv *MainView) bumpStatusLineRight() {
	selectedRow, _ := mv.logsTable.GetSelection()
	selectedRow -= 1

	var selectedRowStr string
	if selectedRow >= 1 {
		selectedRowStr = strconv.Itoa(selectedRow)
	} else {
		selectedRowStr = "-"
	}

	if mv.curLogResp != nil {
		mv.statusLineRight.SetText(fmt.Sprintf(
			"%s / %d / %d",
			selectedRowStr, len(mv.curLogResp.Logs), mv.curLogResp.NumMsgsTotal,
		))
	} else {
		mv.statusLineRight.SetText("-")
	}
}

func newTableCellHeader(text string) *tview.TableCell {
	return tview.NewTableCell(text).
		SetTextColor(tcell.ColorLightBlue).
		SetAttributes(tcell.AttrBold).
		SetAlign(tview.AlignLeft).
		SetSelectable(false)
}

func newTableCellLogmsg(text string) *tview.TableCell {
	return tview.NewTableCell(text).SetTextColor(tcell.ColorWhite).SetAlign(tview.AlignLeft)
}

func newTableCellButton(text string) *tview.TableCell {
	return tview.NewTableCell(text).SetTextColor(tcell.ColorWhite).SetAlign(tview.AlignCenter)
}

/*

	mv.bottomForm = tview.NewForm().
		AddButton("Place order", func() {
			fmt.Println("Place order")
			//msv := NewMarketSelectorView(mv, &MarketSelectorParams{
			//Title: "Place order on which market?",
			//OnSelected: func(marketID common.MarketID) bool {
			//pov := NewPlaceOrderView(mv, &PlaceOrderViewParams{
			//Market: mv.marketDescrByID[marketID],
			//})

			//pov.Show()
			//return true
			//},
			//})
			//msv.Show()
		}).
		AddButton("Cancel order", func() {
			//msv := NewMarketSelectorView(mv, &MarketSelectorParams{
			//Title: "Cancel order on which market?",
			//OnSelected: func(marketID common.MarketID) bool {

			//// Even though we're in the UI loop right now, we can't invoke
			//// FocusOrdersList right here, because when OnSelected returns, we
			//// hide the modal window, and focus will be moved back to the bottom
			//// menu. We need to call FocusOrdersList _after_ that.
			//mv.params.App.QueueUpdateDraw(func() {
			//mv.marketViewsByID[marketID].FocusOrdersList(
			//func(order common.PrivateOrder) {
			//// TODO: confirm
			//mv.params.OnCancelOrderRequest(common.CancelOrderParams{
			//MarketID: marketID,
			//OrderID:  order.ID,
			//})
			//mv.params.App.SetFocus(mv.bottomForm)
			//},
			//func() {
			//mv.params.App.SetFocus(mv.bottomForm)
			//},
			//)
			//})
			//return true
			//},
			//})
			//msv.Show()
		}).
		AddButton("Quit", func() {
			params.App.Stop()
		}).
		AddButton("I said quit", func() {
			params.App.Stop()
		})

	mainFlex.AddItem(mv.bottomForm, 3, 0, false)

*/

func (mv *MainView) setQuery(q string) {
	if mv.queryInput.GetText() != q {
		mv.queryInput.SetText(q)
	}
	mv.query = q
}

func (mv *MainView) setTimeRange(from, to TimeOrDur) {
	if from.IsZero() {
		// TODO: maybe better error handling
		panic("from can't be zero")
	}

	mv.from = from
	mv.to = to

	mv.bumpTimeRange(false)

	rangeDur := mv.actualTo.Sub(mv.actualFrom)

	var timeStr string
	if !mv.to.IsZero() {
		timeStr = fmt.Sprintf("%s to %s (%s)", mv.from.Format(inputTimeLayout), mv.to.Format(inputTimeLayout), formatDuration(rangeDur))
	} else if mv.from.IsAbsolute() {
		timeStr = fmt.Sprintf("%s to now (%s)", mv.from.Format(inputTimeLayout), formatDuration(rangeDur))
	} else {
		timeStr = fmt.Sprintf("last %s", TimeOrDur{Dur: -mv.from.Dur})
	}

	mv.timeLabel.SetText(timeStr)
	mv.topFlex.ResizeItem(mv.timeLabel, len(timeStr), 0)

}

// bumpTimeRange only does something useful if the time is relative to current time.
func (mv *MainView) bumpTimeRange(updateHistogramRange bool) {
	if mv.from.IsZero() {
		panic("should never be here")
	}

	// Since relative durations are relative to current time, only negative values are
	// meaningful, so if it's positive, reverse it.

	if !mv.from.IsAbsolute() && mv.from.Dur > 0 {
		mv.from.Dur = -mv.from.Dur
	}

	if !mv.to.IsAbsolute() && mv.to.Dur > 0 {
		mv.to.Dur = -mv.to.Dur
	}

	mv.actualFrom = mv.from.AbsoluteTime(time.Now())

	if !mv.to.IsZero() {
		mv.actualTo = mv.to.AbsoluteTime(time.Now())
	} else {
		mv.actualTo = time.Now()
	}

	// Snap both actualFrom and actualTo to the 1m grid, rounding forward.
	mv.actualFrom = truncateCeil(mv.actualFrom, 1*time.Minute)
	mv.actualTo = truncateCeil(mv.actualTo, 1*time.Minute)

	// If from is after than to, swap them.
	if mv.actualFrom.After(mv.actualTo) {
		mv.actualFrom, mv.actualTo = mv.actualTo, mv.actualFrom
	}

	// Also update the histogram
	if updateHistogramRange {
		mv.histogram.SetRange(int(mv.actualFrom.Unix()), int(mv.actualTo.Unix()))
	}
}

func truncateCeil(t time.Time, dur time.Duration) time.Time {
	t2 := t.Truncate(dur)
	if t2.Equal(t) {
		return t
	}

	return t2.Add(dur)
}

func (mv *MainView) SetTimeRange(from, to TimeOrDur) {
	mv.params.App.QueueUpdateDraw(func() {
		mv.setTimeRange(from, to)
	})
}

func (mv *MainView) setHostsFilter(s string) {
	mv.hostsFilter = s
}

func (mv *MainView) doQuery() {
	mv.params.OnLogQuery(core.QueryLogsParams{
		From:  mv.actualFrom,
		To:    mv.actualTo,
		Query: mv.query,
	})
}

func (mv *MainView) DoQuery() {
	mv.params.App.QueueUpdateDraw(func() {
		mv.doQuery()
	})
}

func formatDuration(dur time.Duration) string {
	ret := dur.String()

	// Strip useless suffix
	if strings.HasSuffix(ret, "h0m0s") {
		return ret[:len(ret)-4]
	} else if strings.HasSuffix(ret, "m0s") {
		return ret[:len(ret)-2]
	}

	return ret
}

type MessageboxParams struct {
	Buttons         []string
	OnButtonPressed func(label string, idx int)

	Width, Height int

	NoFocus bool

	BackgroundColor tcell.Color
}

func (mv *MainView) showMessagebox(
	msgID, title, message string, params *MessageboxParams,
) *MessageView {
	var msgv *MessageView

	if params == nil {
		params = &MessageboxParams{}
	}

	if params.Buttons == nil {
		params.Buttons = []string{"OK"}
	}

	if params.OnButtonPressed == nil {
		params.OnButtonPressed = func(label string, idx int) {
			msgv.Hide()
		}
	}

	msgv = NewMessageView(mv, &MessageViewParams{
		MessageID:       msgID,
		Title:           title,
		Message:         message,
		Buttons:         params.Buttons,
		OnButtonPressed: params.OnButtonPressed,

		Width:  params.Width,
		Height: params.Height,

		NoFocus: params.NoFocus,

		BackgroundColor: params.BackgroundColor,
	})
	msgv.Show()

	return msgv
}

func (mv *MainView) ShowMessagebox(
	msgID, title, message string, params *MessageboxParams,
) {
	mv.params.App.QueueUpdateDraw(func() {
		mv.showMessagebox(msgID, title, message, params)
	})
}

func (mv *MainView) HideMessagebox(msgID string, popFocusStack bool) {
	mv.params.App.QueueUpdateDraw(func() {
		mv.hideModal(pageNameMessage+msgID, popFocusStack)
	})
}

func (mv *MainView) showModal(name string, primitive tview.Primitive, width, height int, focus bool) {
	mv.modalsFocusStack = append(mv.modalsFocusStack, mv.params.App.GetFocus())

	// Returns a new primitive which puts the provided primitive in the center and
	// sets its size to the given width and height.
	modal := func(p tview.Primitive, width, height int) tview.Primitive {
		return tview.NewGrid().
			SetColumns(0, width, 0).
			SetRows(0, height, 0).
			AddItem(p, 1, 1, 1, 1, 0, 0, true)
	}

	mv.rootPages.AddPage(name, modal(primitive, width, height), true, true)

	if focus {
		mv.params.App.SetFocus(primitive)
	} else {
		mv.popFocusStack()
	}
}

func (mv *MainView) hideModal(name string, popFocusStack bool) {
	prevFocused := mv.params.App.GetFocus()

	mv.rootPages.RemovePage(name)
	if popFocusStack {
		mv.popFocusStack()
	} else {
		// Feels hacky, but I didn't find another way: apparently adding/removing
		// pages inevitably messes with focus, and so if we want to keep it
		// unchanged, we have to set it back manually.
		mv.params.App.SetFocus(prevFocused)
	}
}

func (mv *MainView) popFocusStack() {
	l := len(mv.modalsFocusStack)
	mv.params.App.SetFocus(mv.modalsFocusStack[l-1])
	mv.modalsFocusStack = mv.modalsFocusStack[:l-1]
}

func (mv *MainView) getQueryFull() QueryFull {
	ftr := FromToRange{mv.from, mv.to}
	return QueryFull{
		Time:        ftr.String(),
		Query:       mv.query,
		HostsFilter: mv.hostsFilter,
	}
}
