// scripts/check-docs.mjs
// Validates the bilingual documentation tree contract.
//
// Public API:
//   validateDocsTree(rootDir) -> string[]  (deterministic, sorted diagnostics)
//
// CLI: `node check-docs.mjs [rootDir]` prints diagnostics and exits 0/nonzero.
// Importing this module never executes the CLI.

import { readFileSync, readdirSync, statSync, lstatSync, realpathSync, existsSync } from 'node:fs';
import { join, relative, dirname, basename, resolve, sep, isAbsolute } from 'node:path';
import { pathToFileURL } from 'node:url';

// --- contract constants ----------------------------------------------------

const EXPECTED_DOCS = [
  '1-Getting-Started.md',
  '1-Getting-Started_zh.md',
  '2-Authentication.md',
  '2-Authentication_zh.md',
  '3-Configuration.md',
  '3-Configuration_zh.md',
  '4-Usage.md',
  '4-Usage_zh.md',
  '5-Practical-Guide.md',
  '5-Practical-Guide_zh.md',
  '6-Advanced.md',
  '6-Advanced_zh.md',
  '7-Human-Shortcuts.md',
  '7-Human-Shortcuts_zh.md',
];

const RETIRED_FILENAMES = [
  'README_CN.md',
  'authentication-zh.md',
  'cli-practical-guide.md',
  'cli-best-practices.md',
  'cli-human-shortcuts.md',
];

const FORBIDDEN_REFERENCES = [
  'docs/plans',
  'docs/superpowers',
  'docs/verification',
  'docs/agentic-stage1',
  '.llm/internal-docs',
  '/home/',
  '/data00/',
  'volcengine-cli',
];

// Pairs are identified by their numbered prefix, e.g. "1-Getting-Started".
const PAIR_PREFIXES = [
  '1-Getting-Started',
  '2-Authentication',
  '3-Configuration',
  '4-Usage',
  '5-Practical-Guide',
  '6-Advanced',
  '7-Human-Shortcuts',
];

// Expected navigation routes for each numbered page.
// Each entry: { prev, pair, next } — the three link targets (in order) that
// must appear on both the top and bottom navigation lines.
// Index 0 = page 1, index 6 = page 7.
const NAV_ROUTES_EN = [
  { prev: '../README.md', pair: '1-Getting-Started_zh.md', next: '2-Authentication.md' },
  { prev: '1-Getting-Started.md', pair: '2-Authentication_zh.md', next: '3-Configuration.md' },
  { prev: '2-Authentication.md', pair: '3-Configuration_zh.md', next: '4-Usage.md' },
  { prev: '3-Configuration.md', pair: '4-Usage_zh.md', next: '5-Practical-Guide.md' },
  { prev: '4-Usage.md', pair: '5-Practical-Guide_zh.md', next: '6-Advanced.md' },
  { prev: '5-Practical-Guide.md', pair: '6-Advanced_zh.md', next: '7-Human-Shortcuts.md' },
  { prev: '6-Advanced.md', pair: '7-Human-Shortcuts_zh.md', next: '../README.md' },
];
const NAV_ROUTES_ZH = [
  { prev: '../README_ZH.md', pair: '1-Getting-Started.md', next: '2-Authentication_zh.md' },
  { prev: '1-Getting-Started_zh.md', pair: '2-Authentication.md', next: '3-Configuration_zh.md' },
  { prev: '2-Authentication_zh.md', pair: '3-Configuration.md', next: '4-Usage_zh.md' },
  { prev: '3-Configuration_zh.md', pair: '4-Usage.md', next: '5-Practical-Guide_zh.md' },
  { prev: '4-Usage_zh.md', pair: '5-Practical-Guide.md', next: '6-Advanced_zh.md' },
  { prev: '5-Practical-Guide_zh.md', pair: '6-Advanced.md', next: '7-Human-Shortcuts_zh.md' },
  { prev: '6-Advanced_zh.md', pair: '7-Human-Shortcuts.md', next: '../README_ZH.md' },
];

// --- small markdown helpers (line-oriented, no general parser) -------------

function readText(fullPath) {
  return readFileSync(fullPath, 'utf8');
}

function listDir(fullPath) {
  return readdirSync(fullPath, { withFileTypes: true });
}

// Extract headings: array of { level, text, numericPrefix }
// numericPrefix is the leading "1", "1.1", "2.3" token, or '' if none.
function extractHeadings(content) {
  const lines = content.split(/\r?\n/);
  const headings = [];
  for (const line of lines) {
    const m = line.match(/^(#{1,6})\s+(.*)$/);
    if (!m) continue;
    const level = m[1].length;
    const text = m[2].trim();
    const numMatch = text.match(/^(\d+(?:\.\d+)*)\b/);
    headings.push({ level, text, numericPrefix: numMatch ? numMatch[1] : '' });
  }
  return headings;
}

// Extract fenced code blocks: array of { lang, body }
function extractFences(content) {
  const lines = content.split(/\r?\n/);
  const blocks = [];
  let i = 0;
  while (i < lines.length) {
    const open = lines[i].match(/^```(\w+)?\s*$/);
    if (open) {
      const lang = open[1] || '';
      const bodyLines = [];
      i++;
      while (i < lines.length && !/^```\s*$/.test(lines[i])) {
        bodyLines.push(lines[i]);
        i++;
      }
      // i now points at closing fence (or end of file)
      blocks.push({ lang, body: bodyLines.join('\n') });
      i++; // skip closing fence
    } else {
      i++;
    }
  }
  return blocks;
}

// Extract tables: array of { rows: number, cols: number[] }
// A table is a header row + separator row + data rows, all pipe-delimited.
function extractTables(content) {
  const lines = content.split(/\r?\n/);
  const tables = [];
  let i = 0;
  while (i < lines.length) {
    if (isPipeRow(lines[i]) && i + 1 < lines.length && isSeparatorRow(lines[i + 1])) {
      const colCounts = [];
      // header
      colCounts.push(countCols(lines[i]));
      i += 2; // skip header + separator
      while (i < lines.length && isPipeRow(lines[i])) {
        colCounts.push(countCols(lines[i]));
        i++;
      }
      tables.push({ rows: colCounts.length, cols: colCounts });
    } else {
      i++;
    }
  }
  return tables;
}

function isPipeRow(line) {
  return /^\s*\|.*\|\s*$/.test(line) || /^\s*\|/.test(line);
}

function isSeparatorRow(line) {
  return /^\s*\|?\s*:?-+:?(\s*\|\s*:?-+:?)+\s*\|?\s*$/.test(line);
}

function countCols(line) {
  const trimmed = line.replace(/^\s*\|/, '').replace(/\|\s*$/, '');
  return trimmed.split('|').length;
}

// GitHub-compatible-enough Unicode slugger with duplicate-heading suffixes.
function buildAnchorMap(content) {
  const headings = extractHeadings(content);
  const counts = new Map();
  const map = new Set();
  for (const h of headings) {
    let slug = slugify(h.text);
    if (counts.has(slug)) {
      const n = counts.get(slug);
      counts.set(slug, n + 1);
      slug = `${slug}-${n}`;
    } else {
      counts.set(slug, 1);
    }
    map.add(slug);
  }
  return map;
}

function slugify(text) {
  return text
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s-]/gu, '')
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-+|-+$/g, '');
}

// Split a raw link target into { target, anchor }, filtering external/absolute.
// Returns [] for links that should not be validated (external, absolute, empty).
function parseLinkTarget(raw) {
  let target = raw;
  let anchor = '';
  const hashIdx = target.indexOf('#');
  if (hashIdx >= 0) {
    anchor = target.slice(hashIdx + 1);
    target = target.slice(0, hashIdx);
  }
  if (/^(https?:|mailto:|tel:|ftp:)/i.test(target)) return [];
  if (target.startsWith('/')) return [];
  if (target === '' && anchor === '') return [];
  return [{ target, anchor }];
}

// Strip fenced code blocks, replacing them with blank lines so that link-like
// text inside fences is not treated as real links or reference definitions.
function stripFences(content) {
  const lines = content.split(/\r?\n/);
  const out = [];
  let inFence = false;
  for (const line of lines) {
    if (/^```/.test(line)) {
      inFence = !inFence;
      out.push('');
    } else if (inFence) {
      out.push('');
    } else {
      out.push(line);
    }
  }
  return out.join('\n');
}

// Extract inline Markdown link targets from a single line, in order.
// Returns exact target strings so fragments cannot satisfy an adjacent-page
// route that is required to point to the page itself.
function extractInlineLinkTargets(line) {
  const targets = [];
  const re = /\[([^\]]*)\]\(([^)]*)\)/g;
  let m;
  while ((m = re.exec(line)) !== null) {
    targets.push(m[2].trim());
  }
  return targets;
}

// Find the first navigation line: the first non-empty line after the H1 that
// contains at least one inline link. Returns the line text or null.
function findTopNavLine(content) {
  const lines = content.split(/\r?\n/);
  let pastH1 = false;
  for (const line of lines) {
    if (/^#\s+/.test(line)) { pastH1 = true; continue; }
    if (!pastH1) continue;
    if (line.trim() === '') continue;
    if (/\]\([^)]*\)/.test(line)) return line;
    return null;
  }
  return null;
}

// Find the last navigation line: the last non-empty line of the file that
// contains at least one inline link. Returns the line text or null.
function findBottomNavLine(content) {
  const lines = content.split(/\r?\n/);
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i];
    if (line.trim() === '') continue;
    if (/\]\([^)]*\)/.test(line)) return line;
    return null;
  }
  return null;
}

// Extract reference definitions: lines of the form "[id]: target [\"title\"]".
// Returns a Map of lowercased id -> target string.
function extractReferenceDefinitions(content) {
  const defs = new Map();
  const re = /^\s*\[([^\]]+)\]:\s*(\S+)(?:\s+"[^"]*")?\s*$/;
  for (const line of content.split(/\r?\n/)) {
    const m = line.match(re);
    if (m) defs.set(m[1].toLowerCase(), m[2]);
  }
  return defs;
}

// Extract reference-style links and resolve them via the definition map.
// Handles: [text][id], [text][] (collapsed, id=text), and [id] (shortcut).
//
// To avoid false shortcut matches, reference-definition lines are stripped and
// full/collapsed reference spans are masked with spaces before the shortcut
// scan runs. This ensures [auth] inside [auth][ok] or inside a definition line
// is not treated as a separate shortcut link.
function extractReferenceLinks(content, defs) {
  const links = [];

  // Full and collapsed: [text][id] and [text][]
  const reFull = /\[([^\]]*)\]\s*\[\s*([^\]]*)\s*\]/g;
  let m;
  while ((m = reFull.exec(content)) !== null) {
    const text = m[1];
    const id = (m[2] || text).toLowerCase();
    const target = defs.get(id);
    if (target) links.push(...parseLinkTarget(target));
  }

  // Build a masked copy of the content for shortcut scanning:
  // 1. Remove reference-definition lines entirely.
  // 2. Replace full/collapsed reference spans with spaces so their brackets
  //    cannot be re-matched as shortcuts.
  let masked = content
    .split(/\r?\n/)
    .filter((line) => !/^\s*\[([^\]]+)\]:\s*\S/.test(line))
    .join('\n');
  masked = masked.replace(reFull, (match) => ' '.repeat(match.length));

  // Shortcut: [id] where id is a known definition. Lookahead avoids [text](url).
  const reShort = /\[([^\]]+)\](?!\()/g;
  while ((m = reShort.exec(masked)) !== null) {
    const id = m[1].toLowerCase();
    const target = defs.get(id);
    if (target) links.push(...parseLinkTarget(target));
  }

  return links;
}

// Extract relative inline markdown links: array of { target, anchor }
function extractRelativeLinks(content) {
  const links = [];
  const re = /\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g;
  let m;
  while ((m = re.exec(content)) !== null) {
    links.push(...parseLinkTarget(m[2]));
  }
  return links;
}

// True only if the path exists and is a regular file (no symlink following).
function isRegularFile(fullPath) {
  try {
    return lstatSync(fullPath).isFile();
  } catch {
    return false;
  }
}

// True if the lexically resolved path escapes the repository root.
function escapesRoot(root, fullPath) {
  const rel = relative(root, fullPath);
  return rel.startsWith('..') || isAbsolute(rel);
}

// True if the real (symlink-resolved) path escapes the repository root.
function realEscapesRoot(root, fullPath) {
  try {
    const real = realpathSync(fullPath);
    return escapesRoot(root, real);
  } catch {
    return false;
  }
}

// --- main validator --------------------------------------------------------

export function validateDocsTree(rootDir) {
  const diagnostics = [];
  const push = (file, rule) => diagnostics.push(`${file}: ${rule}`);

  const resolvedRoot = resolve(rootDir);
  let root = resolvedRoot;
  try {
    root = realpathSync(resolvedRoot);
  } catch {
    // Preserve structured diagnostics for missing or unreadable roots.
  }

  // --- Check 1 & 8: root README pair and retired filenames -----------------
  // Enumerate every entry by name regardless of type so symlinks/FIFOs/sockets
  // cannot evade the retired-name checks.
  const rootEntries = safeReaddir(root);
  const rootNames = rootEntries.map((e) => e.name);

  if (!rootNames.includes('README.md')) {
    push('README.md', 'root README.md is missing');
  }
  if (!rootNames.includes('README_ZH.md')) {
    push('README_ZH.md', 'root README_ZH.md is missing');
  }
  // Reject any README variant other than the two allowed, by name and
  // regardless of whether the entry is a regular file, symlink, directory, etc.
  for (const name of rootNames) {
    if (/^README/i.test(name) && name !== 'README.md' && name !== 'README_ZH.md') {
      push(name, `retired or forbidden root README filename; only README.md and README_ZH.md are allowed`);
    }
  }

  // --- Check 2: docs/ contents --------------------------------------------
  // docs/ must be a real directory (lstat, no symlink following) that sits
  // inside the repository root. A symlinked or escaping docs/ is rejected
  // before any enumeration happens.
  const docsDir = join(root, 'docs');
  let docsValid = false;
  if (!existsSync(docsDir)) {
    push('docs/', 'docs/ directory is missing');
  } else {
    let docsKind = 'directory';
    try {
      const st = lstatSync(docsDir);
      if (st.isSymbolicLink()) docsKind = 'symlink';
      else if (!st.isDirectory()) docsKind = 'non-directory entry';
    } catch { /* keep generic */ }
    if (docsKind !== 'directory') {
      push('docs/', `docs/ must be a real directory but is a ${docsKind}`);
    } else if (realEscapesRoot(root, docsDir)) {
      push('docs/', 'docs/ resolves outside the repository root');
    } else {
      docsValid = true;
    }
  }

  if (docsValid) {
    const docsEntries = listDir(docsDir);
    const docsNames = docsEntries.map((e) => e.name);
    const retiredDocs = RETIRED_FILENAMES.filter((n) => n !== 'README_CN.md');

    // Classify every entry by name, regardless of type, so symlinks/FIFOs/
    // sockets cannot evade the contract.
    for (const entry of docsEntries) {
      const name = entry.name;
      // Retired names are rejected by name regardless of entry type.
      if (retiredDocs.includes(name)) {
        push(`docs/${name}`, `retired filename "${name}" must not exist`);
        continue;
      }
      // Expected names are validated for regular-file-ness by registerExpected;
      // skip them here to avoid duplicate diagnostics.
      if (EXPECTED_DOCS.includes(name)) {
        continue;
      }
      // Subdirectories keep the existing diagnostic.
      if (entry.isDirectory()) {
        push(`docs/${name}`, 'docs/ must not contain subdirectories');
        continue;
      }
      // Unexpected regular files.
      if (entry.isFile()) {
        push(`docs/${name}`, `unexpected file in docs/; only the ${EXPECTED_DOCS.length} numbered pair files are allowed`);
        continue;
      }
      // Anything else: symlink, FIFO, socket, device, etc.
      push(`docs/${name}`, `unexpected non-regular entry in docs/; only the ${EXPECTED_DOCS.length} numbered pair files are allowed`);
    }

    // Missing expected files (by name, regardless of type).
    for (const expected of EXPECTED_DOCS) {
      if (!docsNames.includes(expected)) {
        push(`docs/${expected}`, `expected docs file is missing`);
      }
    }
  }

  // --- Collect public file paths for content checks -----------------------
  // Only regular files qualify; directories, symlinks, sockets, etc. are
  // reported as diagnostics and never read.
  const publicFiles = [];
  function registerExpected(rel) {
    const full = join(root, rel);
    if (!existsSync(full)) return false;
    if (isRegularFile(full)) return true;
    let kind = 'non-regular file';
    try {
      const st = lstatSync(full);
      if (st.isDirectory()) kind = 'directory';
      else if (st.isSymbolicLink()) kind = 'symlink';
      else if (st.isSocket()) kind = 'socket';
      else if (st.isFIFO()) kind = 'fifo';
      else if (st.isBlockDevice()) kind = 'block device';
      else if (st.isCharacterDevice()) kind = 'character device';
    } catch { /* keep generic */ }
    push(rel, `expected path must be a regular file but is a ${kind}`);
    return false;
  }
  if (registerExpected('README.md')) publicFiles.push('README.md');
  if (registerExpected('README_ZH.md')) publicFiles.push('README_ZH.md');
  if (docsValid) {
    for (const expected of EXPECTED_DOCS) {
      if (registerExpected(`docs/${expected}`)) publicFiles.push(`docs/${expected}`);
    }
  }

  // --- Check 9: forbidden references in public files ----------------------
  for (const rel of publicFiles) {
    const full = join(root, rel);
    let content;
    try {
      content = readText(full);
    } catch {
      continue;
    }
    for (const ref of FORBIDDEN_REFERENCES) {
      if (content.includes(ref)) {
        push(rel, `forbidden reference "${ref}" must not appear in public docs`);
      }
    }
    // Retired filenames must not appear in public content even as prose text.
    for (const name of RETIRED_FILENAMES) {
      if (content.includes(name)) {
        push(rel, `retired filename "${name}" must not appear in public docs content`);
      }
    }
  }

  // --- Check 3,4,5: pair consistency --------------------------------------
  // Include the root README pair alongside the numbered docs pairs.
  const allPairs = [
    { en: 'README.md', zh: 'README_ZH.md' },
    ...PAIR_PREFIXES.map((p) => ({ en: `docs/${p}.md`, zh: `docs/${p}_zh.md` })),
  ];
  for (const pair of allPairs) {
    const enRel = pair.en;
    const zhRel = pair.zh;
    const enFull = join(root, enRel);
    const zhFull = join(root, zhRel);
    // Only compare when both are regular files; non-regular entries are
    // reported elsewhere and must not be read.
    if (!isRegularFile(enFull) || !isRegularFile(zhFull)) continue;

    let enContent, zhContent;
    try {
      enContent = readText(enFull);
      zhContent = readText(zhFull);
    } catch {
      continue;
    }

    // heading level sequence + numeric prefixes
    const enHeadings = extractHeadings(enContent);
    const zhHeadings = extractHeadings(zhContent);
    const enLevels = enHeadings.map((h) => h.level);
    const zhLevels = zhHeadings.map((h) => h.level);
    if (!arraysEqual(enLevels, zhLevels)) {
      push(enRel, `heading level sequence mismatch with ${zhRel}: [${enLevels.join(',')}] vs [${zhLevels.join(',')}]`);
    }
    const enNums = enHeadings.map((h) => h.numericPrefix);
    const zhNums = zhHeadings.map((h) => h.numericPrefix);
    if (!arraysEqual(enNums, zhNums)) {
      push(enRel, `numeric section prefix mismatch with ${zhRel}: [${enNums.join(',')}] vs [${zhNums.join(',')}]`);
    }

    // fenced code: language + byte-identical body
    const enFences = extractFences(enContent);
    const zhFences = extractFences(zhContent);
    if (enFences.length !== zhFences.length) {
      push(enRel, `fenced code block count mismatch with ${zhRel}: ${enFences.length} vs ${zhFences.length}`);
    } else {
      for (let i = 0; i < enFences.length; i++) {
        if (enFences[i].lang !== zhFences[i].lang) {
          push(enRel, `fenced code block #${i + 1} language mismatch with ${zhRel}: "${enFences[i].lang}" vs "${zhFences[i].lang}"`);
        }
        if (enFences[i].body !== zhFences[i].body) {
          push(enRel, `fenced code block #${i + 1} body mismatch with ${zhRel} (bodies must be byte-identical)`);
        }
      }
    }

    // table shape
    const enTables = extractTables(enContent);
    const zhTables = extractTables(zhContent);
    if (enTables.length !== zhTables.length) {
      push(enRel, `table count mismatch with ${zhRel}: ${enTables.length} vs ${zhTables.length}`);
    } else {
      for (let i = 0; i < enTables.length; i++) {
        const et = enTables[i];
        const zt = zhTables[i];
        if (et.rows !== zt.rows) {
          push(enRel, `table #${i + 1} row count mismatch with ${zhRel}: ${et.rows} vs ${zt.rows}`);
        }
        if (!arraysEqual(et.cols, zt.cols)) {
          push(enRel, `table #${i + 1} column count mismatch with ${zhRel}: [${et.cols.join(',')}] vs [${zt.cols.join(',')}]`);
        }
      }
    }
  }

  // --- Check: numbered-page navigation contract ----------------------------
  // Each numbered page must have a top navigation line (right after H1) and a
  // bottom navigation line (last non-empty line), each with exactly three
  // inline links in order: previous page, own-language counterpart, next page.
  for (let i = 0; i < PAIR_PREFIXES.length; i++) {
    const prefix = PAIR_PREFIXES[i];
    const enRel = `docs/${prefix}.md`;
    const zhRel = `docs/${prefix}_zh.md`;
    const enFull = join(root, enRel);
    const zhFull = join(root, zhRel);
    if (!isRegularFile(enFull) || !isRegularFile(zhFull)) continue;
    let enContent, zhContent;
    try { enContent = readText(enFull); zhContent = readText(zhFull); } catch { continue; }

    const enRoute = NAV_ROUTES_EN[i];
    const zhRoute = NAV_ROUTES_ZH[i];

    for (const [rel, content, route] of [[enRel, enContent, enRoute], [zhRel, zhContent, zhRoute]]) {
      const expected = [route.prev, route.pair, route.next];
      const topLine = findTopNavLine(content);
      if (!topLine) {
        push(rel, 'missing top navigation line after H1');
      } else {
        const targets = extractInlineLinkTargets(topLine);
        if (targets.length !== 3) {
          push(rel, `top navigation line must have exactly 3 links, found ${targets.length}`);
        } else if (!arraysEqual(targets, expected)) {
          push(rel, `top navigation links mismatch: [${targets.join(', ')}] vs expected [${expected.join(', ')}]`);
        }
      }
      const bottomLine = findBottomNavLine(content);
      if (!bottomLine) {
        push(rel, 'missing bottom navigation line at end of file');
      } else {
        const targets = extractInlineLinkTargets(bottomLine);
        if (targets.length !== 3) {
          push(rel, `bottom navigation line must have exactly 3 links, found ${targets.length}`);
        } else if (!arraysEqual(targets, expected)) {
          push(rel, `bottom navigation links mismatch: [${targets.join(', ')}] vs expected [${expected.join(', ')}]`);
        }
      }
    }
  }

  // --- Check 6 & 7: link resolution and language routing ------------------
  // Build anchor maps for all public files
  const anchorMaps = new Map();
  for (const rel of publicFiles) {
    const full = join(root, rel);
    try {
      anchorMaps.set(rel, buildAnchorMap(readText(full)));
    } catch {
      anchorMaps.set(rel, new Set());
    }
  }

  for (const rel of publicFiles) {
    const full = join(root, rel);
    let content;
    try {
      content = readText(full);
    } catch {
      continue;
    }
    // Strip fences so link-like text inside code blocks is ignored.
    const codeFree = stripFences(content);
    const defs = extractReferenceDefinitions(codeFree);
    const links = [
      ...extractRelativeLinks(codeFree),
      ...extractReferenceLinks(codeFree, defs),
    ];
    const isZh = rel.endsWith('_zh.md') || rel === 'README_ZH.md';
    const isRoot = rel === 'README.md' || rel === 'README_ZH.md';
    // own pair counterpart: for numbered pages it is the other language file;
    // for root pages it is the other README.
    let ownCounterpart = null;
    if (isRoot) {
      ownCounterpart = isZh ? 'README.md' : 'README_ZH.md';
    } else if (isZh) {
      ownCounterpart = rel.replace(/_zh\.md$/, '.md');
    } else {
      ownCounterpart = rel.replace(/\.md$/, '_zh.md');
    }

    for (const link of links) {
      const sourceDir = dirname(join(root, rel));

      // Anchor-only link (e.g. "#section") refers to the current page.
      if (link.target === '') {
        if (link.anchor) {
          const anchors = anchorMaps.get(rel) || new Set();
          if (!anchors.has(link.anchor)) {
            push(rel, `anchor "#${link.anchor}" not found in current page`);
          }
        }
        continue;
      }

      const targetFull = resolve(sourceDir, link.target);
      const targetRel = relative(root, targetFull).split(sep).join('/');

      // Lexical containment: reject links that escape the repository root.
      if (escapesRoot(root, targetFull)) {
        push(rel, `relative link "${link.target}" escapes the repository root`);
        continue;
      }

      // Resolution: target file must exist and be a file.
      if (!existsSync(targetFull) || !statSync(targetFull).isFile()) {
        push(rel, `broken relative link "${link.target}" does not resolve to a file`);
        continue;
      }

      // Symlink containment: reject targets that resolve outside the root
      // even though their lexical path sits inside it.
      if (realEscapesRoot(root, targetFull)) {
        push(rel, `link target "${link.target}" is a symlink that resolves outside the repository root`);
        continue;
      }

      // Anchor check
      if (link.anchor) {
        const anchors = anchorMaps.get(targetRel) || new Set();
        if (!anchors.has(link.anchor)) {
          push(rel, `anchor "#${link.anchor}" not found in target "${link.target}"`);
        }
      }

      // Language routing: applies to both root and numbered pages.
      const targetBase = basename(targetRel);
      const targetIsDocs = targetRel.startsWith('docs/');
      const targetIsZh = targetBase.endsWith('_zh.md') || targetRel === 'README_ZH.md';
      const targetIsRootReadme = targetRel === 'README.md' || targetRel === 'README_ZH.md';

      if (targetIsDocs || targetIsRootReadme) {
        const isOwnCounterpart = targetRel === ownCounterpart;
        if (isZh) {
          if (!targetIsZh && !isOwnCounterpart) {
            push(rel, `language routing: Chinese page must not link to English page "${link.target}" (only own-pair counterpart allowed)`);
          }
        } else {
          if (targetIsZh && !isOwnCounterpart) {
            push(rel, `language routing: English page must not link to Chinese page "${link.target}" (only own-pair counterpart allowed)`);
          }
        }
      }
    }
  }

  // --- deterministic ordering --------------------------------------------
  diagnostics.sort();
  return diagnostics;
}

// --- utilities -------------------------------------------------------------

function safeReaddir(dir) {
  try {
    return listDir(dir);
  } catch {
    return [];
  }
}

function arraysEqual(a, b) {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

// --- CLI entry point (only when invoked directly) --------------------------

function isMainModule() {
  try {
    const mainPath = process.argv[1];
    if (!mainPath) return false;
    return pathToFileURL(resolve(mainPath)).href === import.meta.url;
  } catch {
    return false;
  }
}

if (isMainModule()) {
  const rootArg = process.argv[2] || process.cwd();
  const diagnostics = validateDocsTree(rootArg);
  if (diagnostics.length === 0) {
    console.log('docs tree OK');
    process.exit(0);
  } else {
    for (const d of diagnostics) console.error(d);
    console.error(`\n${diagnostics.length} documentation contract violation(s) found.`);
    process.exit(1);
  }
}
