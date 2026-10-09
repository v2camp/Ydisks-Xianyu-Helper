package capability

import (
	"fmt"
	"sort"
)

// Catalog 是能力声明的只读注册表。
//
// 装配期一次性构建后不再变更，因此可被多个消费方并发读取而无需加锁。
// 策略求值只认注册表内的声明：调用方只能提供能力名称，不能自报危险等级或档位要求。
type Catalog struct {
	// specs 是按能力名索引的声明表，构建完成后只读。
	specs map[string]Spec
}

// NewCatalog 用给定声明构建注册表。
//
// 任一声明非法或名称重复时返回错误，不静默跳过：
// 注册期的错误若被吞掉，该能力会「看似存在但求值恒拒绝」，排查成本极高。
func NewCatalog(specs ...Spec) (*Catalog, error) {
	// table 是构建中的名称索引，构建完成后不再修改。
	table := make(map[string]Spec, len(specs))
	// spec 是当前待登记的能力声明。
	for _, spec := range specs {
		if !spec.Valid() {
			return nil, fmt.Errorf("能力声明非法，无法登记：%q", spec.Name)
		}
		// exists 表示同名能力是否已被占用。
		if _, exists := table[spec.Name]; exists {
			return nil, fmt.Errorf("能力名重复，无法登记：%q", spec.Name)
		}
		table[spec.Name] = spec
	}
	return &Catalog{specs: table}, nil
}

// Lookup 按名称查找能力声明；未登记时 ok 为 false。
//
// 接收者为 nil 时同样返回 false，使未装配目录的部署表现为全部拒绝而非 panic。
func (c *Catalog) Lookup(name string) (Spec, bool) {
	if c == nil {
		return Spec{}, false
	}
	// spec 和 ok 分别是命中的声明与命中标志。
	spec, ok := c.specs[name]
	return spec, ok
}

// Names 返回全部已登记的能力名，按字典序排列。
//
// 排序保证同一目录每次输出一致，便于稳定断言与人工比对。
func (c *Catalog) Names() []string {
	if c == nil {
		return nil
	}
	// names 是待排序的能力名集合。
	names := make([]string, 0, len(c.specs))
	// name 是当前遍历到的能力名。
	for name := range c.specs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Len 返回已登记的能力数量。
func (c *Catalog) Len() int {
	if c == nil {
		return 0
	}
	return len(c.specs)
}
