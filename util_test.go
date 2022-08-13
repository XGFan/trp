package trp

import (
	"testing"
)

func TestCircle_Add(t *testing.T) {
	t.Run("simple add 1", func(t *testing.T) {
		c := Circle[int]{}
		c.Add(1)
		if c.Next() != 1 {
			t.Errorf("should get 1")
		}
		if c.Next() != 1 {
			t.Errorf("should get 1")
		}
	})
	t.Run("simple add 1,2,3", func(t *testing.T) {
		c := Circle[int]{}
		c.Add(1)
		c.Add(2)
		c.Add(3)
		stringer := c.Stringer()
		if stringer != "[3 2 1]" {
			t.Errorf("should print [1,2,3], got %s", stringer)
		}
		if c.Next() != 3 {
			t.Errorf("should get 3")
		}
		if c.Next() != 2 {
			t.Errorf("should get 2")
		}
	})
}

func TestCircle_remove(t *testing.T) {
	t.Run("remove empty", func(t *testing.T) {
		c := Circle[int]{}
		c.Remove(1)
	})
	t.Run("remove not exist", func(t *testing.T) {
		c := Circle[int]{}
		c.Add(2)
		c.Remove(1)
	})
	t.Run("remove fist equal", func(t *testing.T) {
		c := Circle[int]{}
		c.Add(1)
		c.Add(3)
		c.Add(2)
		c.Add(3)
		c.Remove(3)
		ret := c.Stringer()
		if ret != "[2 3 1]" {
			t.Errorf("should get [2 3 1], got %s", ret)
		}
	})
	t.Run("remove last", func(t *testing.T) {
		c := Circle[int]{}
		c.Add(1)
		c.Add(3)
		c.Add(2)
		c.Add(3)
		c.Remove(1)
		ret := c.Stringer()
		if ret != "[3 2 3]" {
			t.Errorf("should get [3 2 3], got %s", ret)
		}
	})
	t.Run("remove", func(t *testing.T) {
		c := Circle[int]{}
		c.Add(1)
		c.Add(3)
		c.Add(2)
		c.Add(3)
		c.Remove(2)
		ret := c.Stringer()
		if ret != "[3 3 1]" {
			t.Errorf("should get [3 3 1], got %s", ret)
		}
	})
}

func TestCircle_Next(t *testing.T) {
	t.Run("next and next", func(t *testing.T) {
		c := Circle[int]{}
		c.Add(3)
		c.Add(2)
		c.Add(1)
		if c.Next() != 1 {
			t.Errorf("should get 1")
		}
		if c.Next() != 2 {
			t.Errorf("should get 2")
		}
		if c.Next() != 3 {
			t.Errorf("should get 3")
		}
		if c.Next() != 1 {
			t.Errorf("should get 1")
		}
		c.Add(5)
		if c.Next() != 5 {
			t.Errorf("should get 5")
		}
		if c.Next() != 2 {
			t.Errorf("should get 2")
		}
	})
}
