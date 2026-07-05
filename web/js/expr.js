// expr.js — tiny, safe evaluator for same-page `visible_when` / `required_when`
// expressions (contract.md §3.1).
//
// Deliberately NOT `eval`/`new Function` — this string ultimately comes from
// server data, and the plan calls for XSS review discipline on this surface
// (plan.md §1). A hand-rolled recursive-descent parser over a tiny grammar
// keeps the attack surface to "can read `answers` object properties", nothing
// executable.
//
// Grammar:
//   expr   := or
//   or     := and ('||' and)*
//   and    := cmp ('&&' cmp)*
//   cmp    := primary (('==' | '!=') primary | 'in' list)?
//   primary:= IDENT | STRING | NUMBER | 'true' | 'false' | '(' expr ')'
//   list   := '[' (primary (',' primary)*)? ']'
//
// Contract note: cross-page conditions (`answers.<step>.<field>`) are never
// sent to the client (server resolves those before serving the page) — so
// identifiers here are always plain same-page field keys. If a dotted
// identifier somehow shows up anyway, it resolves to `undefined` rather than
// throwing, and the expression fails safe (visible_when → hidden,
// required_when → not required).

function tokenize(src) {
  const tokens = [];
  let i = 0;
  const n = src.length;
  while (i < n) {
    const c = src[i];
    if (/\s/.test(c)) { i++; continue; }
    if (c === "'" || c === '"') {
      const quote = c;
      let j = i + 1;
      let str = "";
      while (j < n && src[j] !== quote) { str += src[j]; j++; }
      tokens.push({ type: "string", value: str });
      i = j + 1;
      continue;
    }
    if (/[0-9]/.test(c) || (c === "-" && /[0-9]/.test(src[i + 1] || ""))) {
      let j = i + 1;
      while (j < n && /[0-9.]/.test(src[j])) j++;
      tokens.push({ type: "number", value: Number(src.slice(i, j)) });
      i = j;
      continue;
    }
    if (/[a-zA-Z_]/.test(c)) {
      let j = i + 1;
      while (j < n && /[a-zA-Z0-9_.]/.test(src[j])) j++;
      const word = src.slice(i, j);
      if (word === "true" || word === "false") tokens.push({ type: "bool", value: word === "true" });
      else if (word === "in") tokens.push({ type: "in" });
      else tokens.push({ type: "ident", value: word });
      i = j;
      continue;
    }
    if (src.slice(i, i + 2) === "==") { tokens.push({ type: "==" }); i += 2; continue; }
    if (src.slice(i, i + 2) === "!=") { tokens.push({ type: "!=" }); i += 2; continue; }
    if (src.slice(i, i + 2) === "&&") { tokens.push({ type: "&&" }); i += 2; continue; }
    if (src.slice(i, i + 2) === "||") { tokens.push({ type: "||" }); i += 2; continue; }
    if ("[](),".includes(c)) { tokens.push({ type: c }); i++; continue; }
    // Unknown character — bail out of tokenizing; caller treats as unparsable.
    throw new Error(`Unexpected character '${c}' in expression`);
  }
  return tokens;
}

function parse(tokens) {
  let pos = 0;
  const peek = () => tokens[pos];
  const advance = () => tokens[pos++];

  function parsePrimary() {
    const tok = peek();
    if (!tok) throw new Error("Unexpected end of expression");
    if (tok.type === "(") {
      advance();
      const e = parseOr();
      if (peek()?.type !== ")") throw new Error("Expected ')'");
      advance();
      return e;
    }
    if (tok.type === "[") return parseList();
    if (tok.type === "ident") { advance(); return { kind: "ident", name: tok.value }; }
    if (tok.type === "string") { advance(); return { kind: "lit", value: tok.value }; }
    if (tok.type === "number") { advance(); return { kind: "lit", value: tok.value }; }
    if (tok.type === "bool") { advance(); return { kind: "lit", value: tok.value }; }
    throw new Error(`Unexpected token ${tok.type}`);
  }

  function parseList() {
    advance(); // '['
    const items = [];
    if (peek()?.type !== "]") {
      items.push(parsePrimary());
      while (peek()?.type === ",") { advance(); items.push(parsePrimary()); }
    }
    if (peek()?.type !== "]") throw new Error("Expected ']'");
    advance();
    return { kind: "list", items };
  }

  function parseCmp() {
    const left = parsePrimary();
    const tok = peek();
    if (tok && (tok.type === "==" || tok.type === "!=")) {
      advance();
      const right = parsePrimary();
      return { kind: tok.type, left, right };
    }
    if (tok && tok.type === "in") {
      advance();
      const right = parseList();
      return { kind: "in", left, right };
    }
    return left;
  }

  function parseAnd() {
    let left = parseCmp();
    while (peek()?.type === "&&") { advance(); left = { kind: "&&", left, right: parseCmp() }; }
    return left;
  }

  function parseOr() {
    let left = parseAnd();
    while (peek()?.type === "||") { advance(); left = { kind: "||", left, right: parseAnd() }; }
    return left;
  }

  const result = parseOr();
  if (pos !== tokens.length) throw new Error("Trailing tokens in expression");
  return result;
}

function resolve(node, answers) {
  switch (node.kind) {
    case "lit":
      return node.value;
    case "ident":
      return Object.prototype.hasOwnProperty.call(answers, node.name) ? answers[node.name] : undefined;
    case "list":
      return node.items.map((i) => resolve(i, answers));
    case "==":
      return resolve(node.left, answers) === resolve(node.right, answers);
    case "!=":
      return resolve(node.left, answers) !== resolve(node.right, answers);
    case "in": {
      const left = resolve(node.left, answers);
      const list = resolve(node.right, answers);
      return Array.isArray(list) && list.includes(left);
    }
    case "&&":
      return Boolean(resolve(node.left, answers)) && Boolean(resolve(node.right, answers));
    case "||":
      return Boolean(resolve(node.left, answers)) || Boolean(resolve(node.right, answers));
    default:
      return undefined;
  }
}

// Cache compiled ASTs — the same expression string is re-evaluated on every
// keystroke while the user fills the page.
const compiledCache = new Map();

function compile(source) {
  if (compiledCache.has(source)) return compiledCache.get(source);
  let ast = null;
  try {
    ast = parse(tokenize(source));
  } catch (e) {
    console.warn(`[expr] failed to parse "${source}":`, e.message);
    ast = null; // fail safe below
  }
  compiledCache.set(source, ast);
  return ast;
}

// Evaluates `source` against a flat same-page `answers` object. Returns
// `fallback` (default false) if the expression is empty, missing, or fails to
// parse — fail-safe rather than fail-open, per the "server re-verifies
// everything anyway" split in contract.md §3.1.
export function evaluateExpr(source, answers, fallback = false) {
  if (!source) return fallback;
  const ast = compile(source);
  if (!ast) return fallback;
  try {
    return Boolean(resolve(ast, answers));
  } catch (e) {
    console.warn(`[expr] failed to evaluate "${source}":`, e.message);
    return fallback;
  }
}
