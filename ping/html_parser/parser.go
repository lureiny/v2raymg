package htmlparser

import "golang.org/x/net/html"

// 根据 id 查找节点
func FindNodeByID(node *html.Node, id string) *html.Node {
	if node.Type == html.ElementNode && HasIDAttribute(node, id) {
		return node
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		foundNode := FindNodeByID(child, id)
		if foundNode != nil {
			return foundNode
		}
	}

	return nil
}

// 检查节点是否具有匹配的 id 属性
func HasIDAttribute(node *html.Node, id string) bool {
	for _, attr := range node.Attr {
		if attr.Key == "id" && attr.Val == id {
			return true
		}
	}

	return false
}

// 根据类名查找标签
func FindTagsByClass(node *html.Node, className string) []*html.Node {
	var tags []*html.Node

	if node.Type == html.ElementNode && HasClassAttribute(node, className) {
		tags = append(tags, node)
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		tags = append(tags, FindTagsByClass(child, className)...)
	}

	return tags
}

// 检查节点是否具有匹配的类名属性
func HasClassAttribute(node *html.Node, className string) bool {
	for _, attr := range node.Attr {
		if attr.Key == "class" && attr.Val == className {
			return true
		}
	}

	return false
}

// 提取节点的值
func ExtractNodeValue(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Data
	}

	var value string
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		value += ExtractNodeValue(child)
	}

	return value
}
