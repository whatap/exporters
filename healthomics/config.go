package main

import "time"

type config struct {
	region       string
	interval     time.Duration // status polling period
	retention    time.Duration // how long finished runs stay exported after they stop
	maxStaleness time.Duration // how long a snapshot is served while polls fail
	runPages     int           // page cap for the unfiltered ListRuns scan
	taskPages    int           // page cap for ListRunTasks per run

	analyzerDelay    time.Duration // wait after a run stops: the manifest log is written after the run ends
	analyzerRetry    time.Duration // wait between failed analysis attempts of the same run
	analyzerAttempts int           // attempts per run before giving up
}

func defaultConfig() config {
	return config{
		interval:         60 * time.Second,
		retention:        24 * time.Hour,
		maxStaleness:     3 * time.Minute,
		runPages:         5,
		taskPages:        50,
		analyzerDelay:    2 * time.Minute,
		analyzerRetry:    5 * time.Minute,
		analyzerAttempts: 3,
	}
}
