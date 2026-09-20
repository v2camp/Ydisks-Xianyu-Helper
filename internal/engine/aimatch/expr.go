// Package aimatch 提供「组合词」匹配表达式：用布尔运算组合关键词，作为
// AI 客服边界/负向拦截/知识命中的统一判定语言，替代硬编码正则。
//
// 语法（低代码友好）：
//   - |    或       (便宜 | 优惠)
//   - &    且       (砍价 & 元)      相邻词默认按「且」处理（如：百度网盘 发货）
//   - !    非       !(会员 & 有)     置于词或分组前
//   - ( )  分组     支持任意嵌套
//   - 词   普通词按「包含」匹配（忽略大小写），括号/竖线等符号不用转义；
//     用双引号包裹的词「"..."」按正则匹配（用于 \d 等可变数字），
//     正则词内的 ( | 不会被当作语法符号。
//   - 空表达式永远不命中。
package aimatch

import (
	"fmt"
	"regexp"
	"strings"
)

// Compile 把组合词表达式编译成可复用的判定函数。
// expr 为空或全空白时返回永不命中的函数；语法或正则错误时返回错误。
// 返回的函数接收待匹配文本，回传是否命中。
func Compile(expr string) (func(string) bool, error) {
	// tokens 是表达式切分后的单元序列；err 是切分语法错误。
	tokens, err := tokenize(expr)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return func(string) bool { return false }, nil
	}
	// p 是本次解析的位置游标。
	p := parser{tokens: tokens}
	// ast 是解析出的表达式树根节点；err 是解析语法错误。
	ast, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.tokens) {
		// extra 是表达式尾部的多余符号，属于语法错误。
		return nil, fmt.Errorf("组合词表达式语法错误: 多余符号 %+v", p.tokens[p.pos])
	}
	return ast.eval, nil
}

// token 是词法单元：词或运算符。
type token struct {
	// kind 是 tokenOp（运算符）或 tokenWord（词）。
	kind string
	// op 是运算符原始字符，仅运算符单元有效。
	op string
	// word 是词文本，仅词单元有效。
	word string
	// regex 表示该词是否由双引号包裹（按正则匹配而非字面）。
	regex bool
}

// tokenOp 与 tokenWord 区分运算符与关键词单元。
const (
	tokenOp   = "op"
	tokenWord = "word"
)

// operatorChars 是组合词语言保留的运算符字符。
const operatorChars = "|&!()"

// tokenize 把组合词表达式拆成词/运算符序列，并把相邻词之间的空白语义合并为「且」。
// 双引号内（含双引号成对包裹）的内容作为一个整体词按正则匹配，内部符号不参与语法切分。
func tokenize(expr string) ([]token, error) {
	// out 是最终的单元序列。
	var out []token
	// word 是当前累积中的词缓冲。
	var word strings.Builder
	// inQuote 表示当前是否位于双引号之间。
	inQuote := false
	// wordIsRegex 标记当前累积的词是否为引号正则词。
	wordIsRegex := false
	// flush 把已累积的词落盘为 tokenWord 单元。
	flush := func() {
		if word.Len() > 0 {
			// t 是去除首尾空白后的词文本；非空时按词单元落盘。
			t := strings.TrimSpace(word.String())
			if t != "" {
				out = append(out, token{kind: tokenWord, word: t, regex: wordIsRegex})
			}
			word.Reset()
		}
		wordIsRegex = false
	}
	// r 是当前遍历到的字符。
	for _, r := range expr {
		if inQuote {
			// 引号内跳过语法切分；遇到结束引号时整段落盘为正则词。
			if r == '"' {
				inQuote = false
				flush()
			} else {
				word.WriteRune(r)
			}
			continue
		}
		switch {
		case r == '"':
			flush()
			inQuote = true
			wordIsRegex = true
		case strings.ContainsRune(operatorChars, r):
			flush()
			out = append(out, token{kind: tokenOp, op: string(r)})
		case r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '\u3000':
			flush()
		default:
			word.WriteRune(r)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("组合词表达式语法错误: 未闭合的双引号")
	}
	flush()
	return out, nil
}

// parser 是组合词表达式的递归下降解析器。
type parser struct {
	// tokens 是待消费的单元序列。
	tokens []token
	// pos 是当前读取位置。
	pos int
}

// peek 看当前单元；越界返回 nil 单元。
func (p *parser) peek() *token {
	if p.pos >= len(p.tokens) {
		return nil
	}
	return &p.tokens[p.pos]
}

// next 消费并返回当前单元；越界返回 nil。
func (p *parser) next() *token {
	// t 是当前待消费的单元。
	t := p.peek()
	if t != nil {
		p.pos++
	}
	return t
}

// matchOp 判断当前单元是否是指定运算符并消费它。
func (p *parser) matchOp(op string) bool {
	// t 是当前待判断的单元。
	t := p.peek()
	if t != nil && t.kind == tokenOp && t.op == op {
		p.pos++
		return true
	}
	return false
}

// exprNode 是表达式树节点，eval 对指定文本求值。
type exprNode struct {
	// eval 是求值闭包。
	eval func(string) bool
}

// parseOr 解析 or := and ('|' and)*，任一子表达式命中即命中。
func (p *parser) parseOr() (*exprNode, error) {
	// left 是左侧已解析的「且」表达式。
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	// children 收集该层所有 or 分支。
	children := []*exprNode{left}
	for p.matchOp("|") {
		// branch 是当前 or 分支的「且」表达式。
		branch, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		children = append(children, branch)
	}
	return &exprNode{eval: func(text string) bool {
		// node 是当前遍历到的 or 分支。
		for _, node := range children {
			if node.eval(text) {
				return true
			}
		}
		return false
	}}, nil
}

// parseAnd 解析 and := unary ('&' unary | unary)*，相邻词默认「且」。
func (p *parser) parseAnd() (*exprNode, error) {
	// left 是左侧已解析的单元表达式。
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	// children 收集该层所有 and 分支。
	children := []*exprNode{left}
	for {
		// t 是下一个单元；只有词单元或「&」才继续合并为「且」。
		t := p.peek()
		if t != nil && t.kind == tokenOp && t.op == "&" {
			p.pos++
		} else if t == nil || t.kind != tokenWord {
			break
		}
		// branch 是当前 and 分支的单元表达式。
		branch, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		children = append(children, branch)
	}
	return &exprNode{eval: func(text string) bool {
		// node 是当前遍历到的 and 分支。
		for _, node := range children {
			if !node.eval(text) {
				return false
			}
		}
		return true
	}}, nil
}

// parseUnary 解析 unary := '!' unary | '(' expr ')' | word。
func (p *parser) parseUnary() (*exprNode, error) {
	// t 是当前待处理的单元。
	t := p.next()
	if t == nil {
		return nil, fmt.Errorf("组合词表达式语法错误: 表达式意外结束")
	}
	if t.kind == tokenOp && t.op == "!" {
		// inner 是取反的后续单元表达式。
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &exprNode{eval: func(text string) bool {
			return !inner.eval(text)
		}}, nil
	}
	if t.kind == tokenOp && t.op == "(" {
		// inner 是括号内的 or 表达式。
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.matchOp(")") {
			return nil, fmt.Errorf("组合词表达式语法错误: 缺少右括号")
		}
		return inner, nil
	}
	if t.kind != tokenWord {
		return nil, fmt.Errorf("组合词表达式语法错误: 意外的运算符 %q", t.op)
	}
	// word 是当前词的文本；空词直接报错。
	word := strings.TrimSpace(t.word)
	if word == "" {
		return nil, fmt.Errorf("组合词表达式语法错误: 空关键词")
	}
	return compileTerm(word, t.regex)
}

// compileTerm 把词编译为判定函数：正则词按原样编译，普通词转义后按包含匹配。
func compileTerm(word string, regex bool) (*exprNode, error) {
	// expr 是最终参与编译的正则；普通词先转义所有正则元字符再整体加忽略大小写。
	var expr string
	if regex {
		// match 是正则词原文；err 表示语法非法。
		expr = "(?i)" + word
	} else {
		expr = "(?i)" + regexp.QuoteMeta(word)
	}
	// re 是编译后的匹配器；err 表示正则非法。
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("组合词表达式中的关键词 %q 不是合法正则: %w", word, err)
	}
	return &exprNode{eval: re.MatchString}, nil
}
