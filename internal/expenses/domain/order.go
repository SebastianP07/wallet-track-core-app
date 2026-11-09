package domain

const orderIDsMessage = "must list every item of that kind in the month exactly once"

var orderKindViolation = Violation{Field: "kind", Message: "must be one of fixed, occasional, income"}

// OrderedItem is an item as the manual order of its kind sees it (D13).
type OrderedItem struct {
	ID         string
	TemplateID *string
}

// ParseOrderKind accepts the kinds the owner orders by hand: those with their own section (D13).
func ParseOrderKind(raw string) (ItemKind, error) {
	switch kind := ItemKind(raw); kind {
	case ItemFixed, ItemOccasional, ItemIncome:
		return kind, nil
	default:
		return "", &ValidationError{Violations: []Violation{orderKindViolation}}
	}
}

// TemplatesInOrder checks that ids are exactly the month's items of one kind, each once, and returns the
// templates of those items in the new order, for the next month opened to follow it (D13).
func TemplatesInOrder(current []OrderedItem, ids []string) ([]string, error) {
	templateOf := make(map[string]*string, len(current))
	for _, item := range current {
		templateOf[item.ID] = item.TemplateID
	}
	invalid := &ValidationError{Violations: []Violation{{Field: "ids", Message: orderIDsMessage}}}
	if len(ids) != len(current) {
		return nil, invalid
	}
	seen := make(map[string]bool, len(ids))
	templates := make([]string, 0, len(ids))
	for _, id := range ids {
		templateID, known := templateOf[id]
		if !known || seen[id] {
			return nil, invalid
		}
		seen[id] = true
		if templateID != nil {
			templates = append(templates, *templateID)
		}
	}
	return templates, nil
}
