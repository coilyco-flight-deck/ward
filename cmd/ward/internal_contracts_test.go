package main

import (
	"github.com/coilyco-flight-deck/ward/internal/contracts"
	"github.com/coilyco-flight-deck/ward/internal/reviewpanel"
)

var (
	_ contracts.Tracker            = (*forgejoClient)(nil)
	_ contracts.PRRepairClassifier = (*forgejoClient)(nil)
	_ contracts.PRWorkflowClient   = (*forgejoClient)(nil)
	_ contracts.ReviewService      = reviewpanel.Deps{}
)
