package corerag

import "strings"

// Classify assigns a NodeType to a symbol using language-specific heuristics
// based on decorators/annotations and naming conventions (CORE doc §4.2.1
// design principles). The result feeds the compression prior R(k,Type) and
// the SACRS representation family.
//
// Heuristics are deliberately simple and extensible: misclassification falls
// back to NodeOther, which uses conservative priors (CORE doc §7.2).
func Classify(s Symbol, lang string) NodeType {
	switch lang {
	case "go":
		return classifyGo(s)
	case "python":
		return classifyPython(s)
	case "java":
		return classifyJava(s)
	case "typescript", "javascript":
		return classifyTS(s)
	}
	return NodeOther
}

// classifyGo: HTTP handler signatures -> Controller; struct with json tags ->
// DTO; *Service suffix -> Service; util/logger/validator names -> Utility.
func classifyGo(s Symbol) NodeType {
	name := s.Name
	sig := s.Signature
	body := s.Body
	low := strings.ToLower(name)

	if strings.Contains(sig, "http.ResponseWriter") || strings.Contains(sig, "echo.Context") ||
		strings.Contains(sig, "gin.Context") || strings.Contains(sig, "fiber.Ctx") {
		return NodeController
	}
	// Struct with json tags (a DTO/model).
	if s.Kind == "type" && strings.Contains(sig, "struct") &&
		(strings.Contains(body, "json:\"") || strings.Contains(body, "yaml:\"")) {
		return NodeDTO
	}
	if strings.HasSuffix(name, "Service") || strings.HasSuffix(name, "Repository") {
		return NodeService
	}
	// Utility: common peripheral helper names.
	for _, k := range []string{"util", "logger", "log", "validator", "helper", "format"} {
		if strings.Contains(low, k) {
			return NodeUtility
		}
	}
	return NodeOther
}

// classifyPython: route decorators -> Controller; BaseModel/dataclass -> DTO;
// *Service suffix -> Service; util/logger names -> Utility.
func classifyPython(s Symbol) NodeType {
	name := s.Name
	combined := s.Doc + " " + s.Signature + " " + s.Body
	low := strings.ToLower(name)

	if strings.Contains(combined, "@app.route") || strings.Contains(combined, "@router.") ||
		strings.Contains(combined, "@app.get") || strings.Contains(combined, "@app.post") ||
		strings.Contains(combined, "@api_view") {
		return NodeController
	}
	if strings.Contains(combined, "BaseModel") || strings.Contains(combined, "@dataclass") ||
		strings.Contains(combined, "TypedDict") {
		return NodeDTO
	}
	if strings.HasSuffix(name, "Service") {
		return NodeService
	}
	for _, k := range []string{"util", "logger", "log", "validator", "helper", "format"} {
		if strings.Contains(low, k) {
			return NodeUtility
		}
	}
	return NodeOther
}

// classifyJava: Spring @RestController/@Controller -> Controller; @Entity/@Data
// or *Dto suffix -> DTO; @Service/@Repository/@Component -> Service.
func classifyJava(s Symbol) NodeType {
	combined := s.Doc + " " + s.Signature + " " + s.Body
	if strings.Contains(combined, "@RestController") || strings.Contains(combined, "@Controller") ||
		strings.Contains(combined, "@RequestMapping") {
		return NodeController
	}
	if strings.Contains(combined, "@Entity") || strings.Contains(combined, "@Data") ||
		strings.HasSuffix(s.Name, "Dto") || strings.HasSuffix(s.Name, "DTO") {
		return NodeDTO
	}
	if strings.Contains(combined, "@Service") || strings.Contains(combined, "@Repository") ||
		strings.HasSuffix(s.Name, "Service") {
		return NodeService
	}
	if strings.Contains(combined, "@Component") {
		return NodeService
	}
	low := strings.ToLower(s.Name)
	for _, k := range []string{"util", "logger", "log", "validator", "helper"} {
		if strings.Contains(low, k) {
			return NodeUtility
		}
	}
	return NodeOther
}

// classifyTS: Nest @Controller -> Controller; *Dto suffix/interface -> DTO;
// @Injectable or *Service -> Service.
func classifyTS(s Symbol) NodeType {
	combined := s.Doc + " " + s.Signature + " " + s.Body
	if strings.Contains(combined, "@Controller") || strings.Contains(combined, "@Get(") ||
		strings.Contains(combined, "@Post(") {
		return NodeController
	}
	if strings.HasSuffix(s.Name, "Dto") || strings.HasSuffix(s.Name, "DTO") ||
		strings.Contains(s.Signature, "interface ") {
		return NodeDTO
	}
	if strings.Contains(combined, "@Injectable") || strings.HasSuffix(s.Name, "Service") {
		return NodeService
	}
	low := strings.ToLower(s.Name)
	for _, k := range []string{"util", "logger", "log", "validator", "helper", "format"} {
		if strings.Contains(low, k) {
			return NodeUtility
		}
	}
	return NodeOther
}
