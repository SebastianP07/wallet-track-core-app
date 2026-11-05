package kernel

import "time"

// Colombia has stayed at UTC-5, with no daylight saving, since 1993. A fixed zone also spares the
// binary from depending on a tz database in its image.
var bogota = time.FixedZone("America/Bogota", -5*60*60)

// DateInBogota is the calendar date of an instant in America/Bogota, carried at midnight UTC.
func DateInBogota(instant time.Time) time.Time {
	year, month, day := instant.In(bogota).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
