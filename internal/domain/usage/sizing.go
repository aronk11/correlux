package usage

import "strconv"

// SizingLevel says how much a sizing remark matters.
type SizingLevel int

const (
	// SizingFine means there is nothing to say.
	SizingFine SizingLevel = iota
	// SizingNote is money: more reserved than used.
	SizingNote
	// SizingRisk is an outage waiting: memory near its limit, or pods the
	// scheduler places blind.
	SizingRisk
)

// The thresholds. They are deliberately coarse: this is one sample, not a
// week of history, and a remark that fires on a ten percent difference would
// be noise by the next refresh.
const (
	nearLimitPercent = 90  // memory used, as a share of its limit
	idlePercent      = 25  // used, as a share of what is requested
	burstPercent     = 150 // CPU used, as a share of what is requested
	// Below these an application is too small for its reservation to be
	// worth a line.
	minIdleCPUMilli    = 200
	minIdleMemoryBytes = 256 << 20
)

// Sizing compares what an application uses right now with what it reserved
// and what it may not exceed, and says the one thing most worth knowing.
//
// It speaks only when every pod was measured: a total that is missing a pod
// is a floor, and "uses a quarter of what it reserves" built on a floor is a
// recommendation to cut the request of a pod nobody looked at. And it says
// "right now" — a batch job between runs and an API at three in the morning
// both look idle in one sample.
func Sizing(a *AppUsage) (string, SizingLevel) {
	if a.Pods == 0 {
		return "", SizingFine
	}
	if a.Unsized == a.Pods {
		return "no requests set", SizingRisk
	}
	if a.Measured < a.Pods {
		return "", SizingFine
	}

	if a.Limits.HasMemory && a.Used.HasMemory && a.Limits.MemoryBytes > 0 {
		if p := Percent(a.Used.MemoryBytes, a.Limits.MemoryBytes); p >= nearLimitPercent {
			return "memory at " + strconv.Itoa(p) + "% of its limit", SizingRisk
		}
	}
	if a.Requests.HasCPU && a.Used.HasCPU && a.Requests.CPUMilli > 0 {
		if p := Percent(a.Used.CPUMilli, a.Requests.CPUMilli); p >= burstPercent {
			return "CPU at " + strconv.Itoa(p) + "% of its request", SizingNote
		}
	}

	var idle []string
	if a.Requests.HasCPU && a.Used.HasCPU && a.Requests.CPUMilli >= minIdleCPUMilli &&
		Percent(a.Used.CPUMilli, a.Requests.CPUMilli) < idlePercent {
		idle = append(idle, times(a.Requests.CPUMilli, a.Used.CPUMilli)+" CPU")
	}
	if a.Requests.HasMemory && a.Used.HasMemory && a.Requests.MemoryBytes >= minIdleMemoryBytes &&
		Percent(a.Used.MemoryBytes, a.Requests.MemoryBytes) < idlePercent {
		idle = append(idle, times(a.Requests.MemoryBytes, a.Used.MemoryBytes)+" memory")
	}
	switch len(idle) {
	case 1:
		return "reserves " + idle[0] + " of use", SizingNote
	case 2:
		return "reserves " + idle[0] + ", " + idle[1] + " of use", SizingNote
	}
	return "", SizingFine
}

// times renders a ratio as "4×", capped where the number stops meaning
// anything: an idle pod reserving a thousand times its usage is "20×+".
func times(reserved, used int64) string {
	if used <= 0 {
		return "20×+"
	}
	n := reserved / used
	if n >= 20 {
		return "20×+"
	}
	return strconv.FormatInt(n, 10) + "×"
}

// Idle is what the scope reserves and is not using right now, counted only
// over applications every pod of which was measured. Covered says how many
// applications that was, so the number can be qualified.
func Idle(apps []AppUsage) (cpuMilli, memoryBytes int64, covered int) {
	for i := range apps {
		a := &apps[i]
		if a.Pods == 0 || a.Measured < a.Pods {
			continue
		}
		covered++
		if a.Requests.HasCPU && a.Used.HasCPU && a.Requests.CPUMilli > a.Used.CPUMilli {
			cpuMilli += a.Requests.CPUMilli - a.Used.CPUMilli
		}
		if a.Requests.HasMemory && a.Used.HasMemory && a.Requests.MemoryBytes > a.Used.MemoryBytes {
			memoryBytes += a.Requests.MemoryBytes - a.Used.MemoryBytes
		}
	}
	return cpuMilli, memoryBytes, covered
}
