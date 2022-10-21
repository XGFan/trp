package trp

import (
	"reflect"
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

func TestBytesNode_Get(t *testing.T) {
	bn := LinkSlice[byte]{
		Head: []byte{1, 2, 3, 4},
		Tail: []byte{5, 6, 7, 8},
	}
	t.Run("get in head", func(t *testing.T) {
		if bn.Get(0) != 1 {
			t.Errorf("should get 1")
		}
		if bn.Get(3) != 4 {
			t.Errorf("should get 4")
		}
	})
	t.Run("get in tail", func(t *testing.T) {
		if bn.Get(4) != 5 {
			t.Errorf("should get 5")
		}
		if bn.Get(7) != 8 {
			t.Errorf("should get 8")
		}
	})
}

func TestBytesNode_Bytes(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		bn := LinkSlice[byte]{
			Head: []byte{1, 2, 3, 4},
			Tail: []byte{5, 6, 7, 8},
		}
		if !reflect.DeepEqual(bn.Data(), []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
			t.Errorf("should equal")
		}
	})
	t.Run("only head", func(t *testing.T) {
		bn := LinkSlice[byte]{
			Head: []byte{1, 2, 3, 4},
		}
		if !reflect.DeepEqual(bn.Data(), []byte{1, 2, 3, 4}) {
			t.Errorf("should equal")
		}
	})
}

func TestBytesNode_SubFromStart(t *testing.T) {
	bn := LinkSlice[byte]{
		Head: []byte{1, 2, 3, 4},
		Tail: []byte{5, 6, 7, 8},
	}
	t.Run("sub start in head", func(t *testing.T) {
		if !reflect.DeepEqual(bn.SubFromStart(0).Data(), []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
			t.Errorf("should equal")
		}
	})
	t.Run("sub start in head", func(t *testing.T) {
		if !reflect.DeepEqual(bn.SubFromStart(3).Data(), []byte{4, 5, 6, 7, 8}) {
			t.Errorf("should equal, but got %v", bn.SubFromStart(3).Data())
		}
	})
	t.Run("sub start in tail", func(t *testing.T) {
		if !reflect.DeepEqual(bn.SubFromStart(4).Data(), []byte{5, 6, 7, 8}) {
			t.Errorf("should equal, but got %v", bn.SubFromStart(4).Data())
		}
	})
	t.Run("sub start in tail", func(t *testing.T) {
		if !reflect.DeepEqual(bn.SubFromStart(7).Data(), []byte{8}) {
			t.Errorf("should equal, but got %v", bn.SubFromStart(7).Data())
		}
	})
	t.Run("sub start empty", func(t *testing.T) {
		if !reflect.DeepEqual(bn.SubFromStart(8).Data(), []byte{}) {
			t.Errorf("should equal, but got %v", bn.SubFromStart(8).Data())
		}
	})
}

func TestBytesNode_SubToEnd(t *testing.T) {
	bn := LinkSlice[byte]{
		Head: []byte{1, 2, 3, 4},
		Tail: []byte{5, 6, 7, 8},
	}
	t.Run("sub end in head", func(t *testing.T) {
		ret := bn.SubToEnd(1).Data()
		if !reflect.DeepEqual(ret, []byte{1}) {
			t.Errorf("should equal, but got %v", ret)
		}
	})
	t.Run("sub end in head", func(t *testing.T) {
		ret := bn.SubToEnd(3).Data()
		if !reflect.DeepEqual(ret, []byte{1, 2, 3}) {
			t.Errorf("should equal, but got %v", ret)
		}
	})
	t.Run("sub end in tail", func(t *testing.T) {
		ret := bn.SubToEnd(5).Data()
		if !reflect.DeepEqual(ret, []byte{1, 2, 3, 4, 5}) {
			t.Errorf("should equal, but got %v", ret)
		}
	})
	t.Run("sub end in tail", func(t *testing.T) {
		ret := bn.SubToEnd(7).Data()
		if !reflect.DeepEqual(ret, []byte{1, 2, 3, 4, 5, 6, 7}) {
			t.Errorf("should equal, but got %v", ret)
		}
	})
	t.Run("sub empty", func(t *testing.T) {
		ret := bn.SubToEnd(0).Data()
		if !reflect.DeepEqual(ret, []byte{}) {
			t.Errorf("should equal, but got %v", ret)
		}
	})
}

func TestBytesNode_Sub(t *testing.T) {
	bn := LinkSlice[byte]{
		Head: []byte{1, 2, 3, 4},
		Tail: []byte{5, 6, 7, 8},
	}
	t.Run("sub in head", func(t *testing.T) {
		ret := bn.Sub(1, 3).Data()
		if !reflect.DeepEqual(ret, []byte{2, 3}) {
			t.Errorf("should equal, but got %v", ret)
		}
	})
	t.Run("sub in tail", func(t *testing.T) {
		ret := bn.Sub(5, 7).Data()
		if !reflect.DeepEqual(ret, []byte{6, 7}) {
			t.Errorf("should equal, but got %v", ret)
		}
	})
	t.Run("sub in head and tail", func(t *testing.T) {
		ret := bn.Sub(2, 6).Data()
		if !reflect.DeepEqual(ret, []byte{3, 4, 5, 6}) {
			t.Errorf("should equal, but got %v", ret)
		}
	})
	t.Run("sub empty", func(t *testing.T) {
		ret := bn.Sub(7, 7).Data()
		if !reflect.DeepEqual(ret, []byte{}) {
			t.Errorf("should equal, but got %v", ret)
		}
		ret = bn.Sub(1, 1).Data()
		if !reflect.DeepEqual(ret, []byte{}) {
			t.Errorf("should equal, but got %v", ret)
		}
		ret = bn.Sub(0, 0).Data()
		if !reflect.DeepEqual(ret, []byte{}) {
			t.Errorf("should equal, but got %v", ret)
		}
	})

}
