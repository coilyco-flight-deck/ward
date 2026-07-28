package main

import "strconv"

func wardIssueURL(number int) string {
	return forgejoBaseURL + "/coilyco-flight-deck/ward/issues/" + strconv.Itoa(number)
}
