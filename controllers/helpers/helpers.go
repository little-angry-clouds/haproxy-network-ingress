/*
Copyright 2019 Little Angry Clouds Inc.
*/

package helper

import (
	corev1 "k8s.io/api/core/v1"
)

// GetMapDifferences returns the differences between two maps.
// Source https://stackoverflow.com/questions/19374219/how-to-find-the-difference-between-two-slices-of-strings-in-golang
func GetMapDifferences(a, b []string) []string {
	mb := make(map[string]struct{}, len(b))
	for _, x := range b {
		mb[x] = struct{}{}
	}
	var diff []string
	for _, x := range a {
		if _, found := mb[x]; !found {
			diff = append(diff, x)
		}
	}
	return diff
}

// ByName sorts services by ContainerPort names
type ByName []corev1.ContainerPort

func (a ByName) Len() int           { return len(a) }
func (a ByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByName) Less(i, j int) bool { return a[i].Name < a[j].Name }
