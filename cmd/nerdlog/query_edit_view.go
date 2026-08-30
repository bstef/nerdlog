package main

import (
	"fmt"

	"github.com/dimonomid/nerdlog/clhistory"
	"github.com/gdamore/tcell/v2"
	"github.com/juju/errors"
	"github.com/rivo/tview"
)

var queryLabelText = `awk pattern. Examples: "[yellow]/foo bar/[-]", or "[yellow]( /foo bar/ || /other stuff/ ) && !/baz/[-]"`

/*
var timeLabelText = `Time range. Both "From" and "To" can either be absolute like "[yellow]Mar27_12:00[-]", or relative
like "[yellow]-2h30m[-]" (relative to current time). The "To" can also be "now" or just an empty string,
in which case the current time will be used. Absolute time is in UTC.
`
*/

var timeLabelText = `Time range in the format "[yellow]<time>[ to <time>][-]", where [yellow]<time>[-] is either absolute like "[yellow]Mar27 12:00[-]"
or relative like "[yellow]-2h30m[-]" (relative to current time). If the "to" part is omitted,
current time is used.
`

var lstreamsLabelText = `Logstreams. Comma-separated strings in the format "[yellow][user@]myserver.com[:port[:/path/to/logfile]][-]"
Examples: "[yellow]user@myserver.com[-]", or "[yellow]user@myserver.com:22:/var/log/syslog[-]"`

var selectQueryLabelText = `Select field expression. Example: "[yellow]time STICKY, message, lstream, level_name AS level, *[-]".`

type QueryEditViewParams struct {
	// DoneFunc is called when the user submits the form. If it returns a non-nil
	// error, the form will show that error and will not be submitted.
	DoneFunc func(data QueryFull, dqp doQueryParams) error

	// TODO: callback for editing nodes, to show in realtime how many nodes matched

	History *clhistory.CLHistory
}

type QueryEditView struct {
	params   QueryEditViewParams
	mainView *MainView

	flex *tview.Flex

	backBtn *tview.Button
	fwdBtn  *tview.Button

	timeFlex      *tview.Flex
	timeInput     *tview.InputField
	timezoneLabel *tview.TextView
	lstreamsInput *tview.InputField
	queryInput    *tview.InputField

	selectQueryInput   *tview.InputField
	selectQueryEditBtn *tview.Button

	frame *tview.Frame
	//
	//textView *tview.TextView
	//buttons  []*tview.Button
}

func NewQueryEditView(
	mainView *MainView, params *QueryEditViewParams,
) *QueryEditView {
	qev := &QueryEditView{
		params:   *params,
		mainView: mainView,
	}

	//if qev.params.Width == 0 {
	//qev.params.Width = 40
	//}

	//if qev.params.Height == 0 {
	//qev.params.Height = 10
	//}

	var focusers []tview.Primitive
	getGenericTabHandler := func(curPrimitive tview.Primitive) func(event *tcell.EventKey) *tcell.EventKey {
		return func(event *tcell.EventKey) *tcell.EventKey {
			key := event.Key()

			nextIdx := 0
			prevIdx := 0

			for i, p := range focusers {
				if p != curPrimitive {
					continue
				}

				prevIdx = i - 1
				if prevIdx < 0 {
					prevIdx = len(focusers) - 1
				}

				nextIdx = i + 1
				if nextIdx >= len(focusers) {
					nextIdx = 0
				}
			}

			switch key {
			case tcell.KeyTab:
				qev.mainView.params.App.SetFocus(focusers[nextIdx])
				return nil

			case tcell.KeyBacktab:
				qev.mainView.params.App.SetFocus(focusers[prevIdx])
				return nil
			}

			return event
		}
	}

	qev.flex = tview.NewFlex().SetDirection(tview.FlexRow)

	historyLabel := tview.NewTextView()
	historyLabel.SetText("Query history:")

	qev.backBtn = tview.NewButton("Back <Ctrl+K>")
	focusers = append(focusers, qev.backBtn)

	andLabel := tview.NewTextView()
	andLabel.SetText("and")

	qev.fwdBtn = tview.NewButton("Forth <Ctrl+J>")
	focusers = append(focusers, qev.fwdBtn)

	topFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	topFlex.
		AddItem(historyLabel, 15, 0, false).
		AddItem(qev.backBtn, 15, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(andLabel, 3, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(qev.fwdBtn, 16, 0, false).
		AddItem(nil, 0, 1, false)
	qev.flex.AddItem(topFlex, 1, 0, false)
	qev.flex.AddItem(nil, 1, 0, false)

	timeLabel := tview.NewTextView()
	timeLabel.SetText(timeLabelText)
	timeLabel.SetDynamicColors(true)
	qev.flex.AddItem(timeLabel, 3, 0, false)

	qev.timeInput = tview.NewInputField()
	focusers = append(focusers, qev.timeInput)

	qev.timezoneLabel = tview.NewTextView()

	qev.timeFlex = tview.NewFlex().SetDirection(tview.FlexColumn)
	qev.timeFlex.
		AddItem(qev.timeInput, 0, 1, true).
		AddItem(nil, 1, 0, false).
		AddItem(qev.timezoneLabel, 0, 0, false) // Will be resized later in SetQueryFull
	qev.flex.AddItem(qev.timeFlex, 1, 0, true)

	qev.flex.AddItem(nil, 1, 0, false)

	lstreamsLabel := tview.NewTextView()
	lstreamsLabel.SetText(lstreamsLabelText)
	lstreamsLabel.SetDynamicColors(true)
	qev.flex.AddItem(lstreamsLabel, 2, 0, false)

	qev.lstreamsInput = tview.NewInputField()
	qev.flex.AddItem(qev.lstreamsInput, 1, 0, false)
	focusers = append(focusers, qev.lstreamsInput)

	qev.flex.AddItem(nil, 1, 0, false)

	queryLabel := tview.NewTextView()
	queryLabel.SetText(queryLabelText)
	queryLabel.SetDynamicColors(true)
	qev.flex.AddItem(queryLabel, 1, 0, false)

	qev.queryInput = tview.NewInputField()
	qev.flex.AddItem(qev.queryInput, 1, 0, false)
	focusers = append(focusers, qev.queryInput)

	qev.flex.AddItem(nil, 1, 0, false)

	selectQueryLabel := tview.NewTextView()
	selectQueryLabel.SetText(selectQueryLabelText)
	selectQueryLabel.SetDynamicColors(true)
	qev.flex.AddItem(selectQueryLabel, 1, 0, false)

	qev.selectQueryInput = tview.NewInputField()
	focusers = append(focusers, qev.selectQueryInput)

	qev.selectQueryEditBtn = tview.NewButton("Edit")
	focusers = append(focusers, qev.selectQueryEditBtn)

	sqFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	sqFlex.
		AddItem(qev.selectQueryInput, 0, 1, false).
		AddItem(nil, 1, 0, false).
		AddItem(qev.selectQueryEditBtn, 6, 0, false)
	qev.flex.AddItem(sqFlex, 1, 0, false)

	//qev.textView = tview.NewTextView()
	//qev.textView.SetText(params.Message)
	//qev.textView.SetTextAlign(tview.AlignCenter)

	//qev.flex.AddItem(qev.textView, 0, 1, len(params.Buttons) == 0)

	//for i, b := range params.Buttons {
	//btnLabel := b
	//btnIdx := i
	//btn := tview.NewButton(btnLabel).SetSelectedFunc(func() {
	//params.OnButtonPressed(btnLabel, btnIdx)
	//})

	//// TODO: use horizontal flex for buttons
	//qev.flex.AddItem(btn, 1, 0, i == 0)
	//}

	qev.frame = tview.NewFrame(qev.flex).SetBorders(0, 0, 0, 0, 0, 0)
	qev.frame.SetBorder(true).SetBorderPadding(1, 1, 1, 1)
	qev.frame.SetTitle("Edit query params")

	qev.backBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			// Just pretend it was a Ctrl+K, and let the genericInputHandler handle it.
			event = tcell.NewEventKey(tcell.KeyCtrlK, 0, tcell.ModNone)
		}

		event = qev.genericInputHandler(event, getGenericTabHandler(qev.backBtn), nil, nil)
		if event == nil {
			return nil
		}

		return event
	})

	qev.fwdBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			// Just pretent it was a Ctrl+J, and let the genericInputHandler handle it.
			event = tcell.NewEventKey(tcell.KeyCtrlJ, 0, tcell.ModNone)
		}

		event = qev.genericInputHandler(event, getGenericTabHandler(qev.fwdBtn), nil, nil)
		if event == nil {
			return nil
		}

		return event
	})

	qev.timeInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		event = qev.genericInputHandler(
			event,
			getGenericTabHandler(qev.timeInput),
			func(qf QueryFull) string { return qf.Time },
			func(qf *QueryFull, part string) { qf.Time = part },
		)
		if event == nil {
			return nil
		}

		switch event.Key() {
		case tcell.KeyEnter:
			if err := qev.applyQuery(); err != nil {
				qev.mainView.handleQueryError(err)
			}
			return nil
		}

		return event
	})

	qev.lstreamsInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		event = qev.genericInputHandler(
			event,
			getGenericTabHandler(qev.lstreamsInput),
			func(qf QueryFull) string { return qf.LStreams },
			func(qf *QueryFull, part string) { qf.LStreams = part },
		)
		if event == nil {
			return nil
		}

		switch event.Key() {
		case tcell.KeyEnter:
			if err := qev.applyQuery(); err != nil {
				qev.mainView.handleQueryError(err)
			}
			return nil
		}

		return event
	})

	qev.queryInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		event = qev.genericInputHandler(
			event,
			getGenericTabHandler(qev.queryInput),
			func(qf QueryFull) string { return qf.Query },
			func(qf *QueryFull, part string) { qf.Query = part },
		)
		if event == nil {
			return nil
		}

		switch event.Key() {
		case tcell.KeyEnter:
			if err := qev.applyQuery(); err != nil {
				qev.mainView.handleQueryError(err)
			}
			return nil
		}

		return event
	})

	qev.selectQueryInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		event = qev.genericInputHandler(
			event,
			getGenericTabHandler(qev.selectQueryInput),
			func(qf QueryFull) string { return string(qf.SelectQuery) },
			func(qf *QueryFull, part string) { qf.SelectQuery = SelectQuery(part) },
		)
		if event == nil {
			return nil
		}

		switch event.Key() {
		case tcell.KeyEnter:
			if err := qev.applyQuery(); err != nil {
				qev.mainView.handleQueryError(err)
			}
			return nil
		}

		return event
	})

	qev.selectQueryEditBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			rdv := NewRowDetailsView(qev.mainView, &RowDetailsViewParams{
				DoneFunc: func(data QueryFull, dqp doQueryParams) error {
					qev.SetQueryFull(data)
					return nil
				},
				Data:             qev.GetQueryFull(),
				ExistingNamesSet: qev.mainView.existingTagNames,
			})
			rdv.Show()
		}

		event = qev.genericInputHandler(event, getGenericTabHandler(qev.selectQueryEditBtn), nil, nil)
		if event == nil {
			return nil
		}

		return event
	})

	return qev
}

func (qev *QueryEditView) Show(data QueryFull) {
	qev.SetQueryFull(data)
	qev.mainView.showModal(
		pageNameEditQueryParams, qev.frame,
		105,
		20,
		true,
	)
}

func (qev *QueryEditView) Hide() {
	qev.mainView.hideModal(pageNameEditQueryParams, true)
}

func (qev *QueryEditView) GetQueryFull() QueryFull {
	return QueryFull{
		Time:        qev.timeInput.GetText(),
		Query:       qev.queryInput.GetText(),
		LStreams:    qev.lstreamsInput.GetText(),
		SelectQuery: SelectQuery(qev.selectQueryInput.GetText()),
	}
}

func (qev *QueryEditView) SetQueryFull(qf QueryFull) {
	qev.timeInput.SetText(qf.Time)
	qev.lstreamsInput.SetText(qf.LStreams)
	qev.queryInput.SetText(qf.Query)

	qev.selectQueryInput.SetText(string(qf.SelectQuery))

	// TODO: instead of using now, better use beginning or end of the time range.
	referenceTime := qev.mainView.params.Clock.Now()
	qev.timezoneLabel.SetText(fmt.Sprintf("(%s)", qev.mainView.timezoneStr(referenceTime)))
	qev.timeFlex.ResizeItem(qev.timezoneLabel, len(qev.timezoneLabel.GetText(true)), 0)
}

func (qev *QueryEditView) genericInputHandler(
	event *tcell.EventKey,
	genericTabHandler func(event *tcell.EventKey) *tcell.EventKey,
	getQFPart func(qf QueryFull) string,
	setQFPart func(qf *QueryFull, part string),
) *tcell.EventKey {
	event = genericTabHandler(event)
	if event == nil {
		return nil
	}

	qf := qev.GetQueryFull()
	cmd := qf.MarshalShellCmd()

	var itemToUse *clhistory.Item

	switch event.Key() {

	// On Ctrl+K, Ctrl+J list history over all fields.

	case tcell.KeyCtrlK:
		item, _ := qev.params.History.Prev(cmd)
		itemToUse = &item

	case tcell.KeyCtrlJ:
		item, _ := qev.params.History.Next(cmd)
		itemToUse = &item

	// On Ctrl+P, Ctrl+N list history over only the current field.  This is kind
	// of a hack since we're still using the common history, and manually
	// skipping the items with the same values for this particular field.  Maybe
	// it'd be easier to just keep separate history files for every field, idk.

	case tcell.KeyCtrlP, tcell.KeyUp, tcell.KeyCtrlN, tcell.KeyDown:
		if getQFPart != nil && setQFPart != nil {
			var item clhistory.Item
			qfPart := getQFPart(qf)

			for {
				var hasMore bool
				if event.Key() == tcell.KeyCtrlP || event.Key() == tcell.KeyUp {
					item, hasMore = qev.params.History.Prev(cmd)
				} else {
					item, hasMore = qev.params.History.Next(cmd)
				}

				var tmp QueryFull
				if err := tmp.UnmarshalShellCmd(item.Str); err != nil {
					qev.mainView.showMessagebox("err", "Broken query history", err.Error(), nil)
					return nil
				}

				curQFPart := getQFPart(tmp)
				if (curQFPart != "" && curQFPart != qfPart) || !hasMore {
					// Either we found a different value for this field, or ran out of
					// history. Set this value in the original QueryFull, and use it.
					setQFPart(&qf, curQFPart)
					item.Str = qf.MarshalShellCmd()
					break
				}
			}

			itemToUse = &item
		}

	case tcell.KeyEsc:
		qev.Hide()
		return nil
	}

	if itemToUse != nil {
		if err := qf.UnmarshalShellCmd(itemToUse.Str); err != nil {
			qev.mainView.showMessagebox("err", "Broken query history", err.Error(), nil)
			return nil
		}

		qev.SetQueryFull(qf)

		return nil
	}

	// If the field was edited by the user, reset the history current position.
	switch event.Key() {
	case tcell.KeyRune, tcell.KeyBackspace, tcell.KeyBackspace2,
		tcell.KeyDelete, tcell.KeyCtrlD,
		tcell.KeyCtrlW, tcell.KeyCtrlU, tcell.KeyCtrlK:

		qev.params.History.Reset()
	}

	return event
}

func (qev *QueryEditView) applyQuery() error {
	err := qev.params.DoneFunc(qev.GetQueryFull(), doQueryParams{})
	if err != nil {
		return errors.Trace(err)
	}

	qev.Hide()

	return nil
}
