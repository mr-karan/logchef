package alerts

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const maxDeliveryErrorBytes = 512

func boundedDeliveryError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToValidUTF8(err.Error(), "?")
	if len(message) <= maxDeliveryErrorBytes {
		if message == err.Error() {
			return err
		}
		return errors.New(message)
	}
	const marker = " [truncated]"
	limit := maxDeliveryErrorBytes - len(marker)
	for !utf8.ValidString(message[:limit]) {
		limit--
	}
	return errors.New(message[:limit] + marker)
}
