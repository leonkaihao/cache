package model

import "strings"

type LabelSet map[string]bool

func (ls LabelSet) List() []string {
	result := []string{}
	for k := range ls {
		result = append(result, k)
	}
	return result
}

func (ls LabelSet) From(labels []string) LabelSet {
	for _, label := range labels {
		if label != "" {
			ls[label] = true
		}
	}
	return ls
}

func (ls LabelSet) CheckAnd(labels []string) bool {
	for _, label := range labels {
		if _, ok := ls[label]; !ok {
			return false
		}
	}
	return true
}

func (ls LabelSet) CheckOr(labels []string) bool {
	for _, label := range labels {
		if _, ok := ls[label]; ok {
			return true
		}
	}
	return false
}

func (ls LabelSet) FromStr(labels string) LabelSet {
	for _, label := range strings.Split(labels, ",") {
		if label != "" {
			ls[label] = true
		}
	}
	return ls
}

func (ls LabelSet) Format() string {
	ret := ""
	for k := range ls {
		if len(ret) == 0 {
			ret += k
		} else {
			ret = ret + "," + k
		}
	}
	return ret
}

func (ls LabelSet) Copy() LabelSet {
	result := LabelSet{}
	for k, v := range ls {
		result[k] = v
	}
	return result
}
