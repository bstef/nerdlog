package main

import (
	"strings"
	"time"

	"github.com/dimonomid/nerdlog/core"
	"github.com/juju/errors"
)

type FromToRange struct {
	From TimeOrDur
	To   TimeOrDur
}

func ParseFromToRange(timezone *time.Location, now time.Time, s string) (FromToRange, error) {
	flds := strings.Split(s, " to ")
	if len(flds) == 0 {
		return FromToRange{}, errors.New("time can't be empty. try -5h")
	}

	var from, to TimeOrDur
	var err error

	fromStr := flds[0]

	from, err = parseAndInferTimeOrDur(timezone, now, inputTimeLayout, fromStr)
	if err != nil {
		return FromToRange{}, errors.Annotatef(err, "invalid 'from' duration")
	}

	to = TimeOrDur{}

	if len(flds) > 1 {
		toStr := flds[1]

		// If there's no date, prepend date
		if len(toStr) <= 5 && len(fromStr) > 5 {
			toStr = fromStr[:5] + " " + toStr
		}

		var err error
		to, err = parseAndInferTimeOrDur(timezone, now, inputTimeLayout, toStr)
		if err != nil {
			return FromToRange{}, errors.Annotatef(err, "invalid 'to' duration")
		}
	}

	return FromToRange{
		From: from,
		To:   to,
	}, nil
}

func (ftr *FromToRange) String() string {
	fromStr := ftr.From.Format(inputTimeLayout)

	if ftr.To.IsZero() {
		return fromStr
	}

	// If both From and To are absolute and have the same day, then omit day for
	// the To.
	format := inputTimeLayout
	_, fm, fd := ftr.From.Time.Date()
	_, tm, td := ftr.To.Time.Date()
	if fm == tm && fd == td {
		format = inputTimeLayoutMMHH
	}

	return fromStr + " to " + ftr.To.Format(format)
}

func parseAndInferTimeOrDur(timezone *time.Location, now time.Time, layout, s string) (TimeOrDur, error) {
	t, err := ParseTimeOrDur(timezone, layout, s)
	if err != nil {
		return TimeOrDur{}, err
	}

	if t.IsAbsolute() {
		t.Time = core.InferYear(now, t.Time)
	}

	return t, nil
}
