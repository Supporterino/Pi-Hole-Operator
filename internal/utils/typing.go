package utils

func Int32Ptr(i int32) *int32 { return &i }

func PtrTo[T any](v T) *T { return &v }
