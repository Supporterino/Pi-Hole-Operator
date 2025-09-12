package controller

func ptrTo[T any](v T) *T { return &v }

func int32Ptr(i int32) *int32 { return &i }
