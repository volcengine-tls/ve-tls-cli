// Tests for scripts/check-docs.mjs
// Strict TDD: these tests define the contract before implementation.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, mkdirSync, rmSync, symlinkSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const { validateDocsTree } = await import('./check-docs.mjs');

// --- helpers ---------------------------------------------------------------

function makeTree(files) {
  const dir = mkdtempSync(join(tmpdir(), 'check-docs-'));
  for (const [relPath, content] of Object.entries(files)) {
    const full = join(dir, relPath);
    mkdirSync(join(full, '..'), { recursive: true });
    writeFileSync(full, content, 'utf8');
  }
  return dir;
}

function cleanup(dir) {
  rmSync(dir, { recursive: true, force: true });
}

// A minimal valid pair used across tests.
const VALID_PAIR_EN = `# 1 Getting Started

[← Previous](../README.md) | [中文](1-Getting-Started_zh.md) | [Next →](2-Authentication.md)

Intro text.

## 1.1 Install

Run:

\`\`\`bash
npm install -g volclog
\`\`\`

## 1.2 Configure

| Option | Description |
| --- | --- |
| --ak | Access key |
| --sk | Secret key |

See [2-Authentication.md](2-Authentication.md).

---

[← Previous](../README.md) | [中文](1-Getting-Started_zh.md) | [Next →](2-Authentication.md)
`;

const VALID_PAIR_ZH = `# 1 快速开始

[← 上一篇](../README_ZH.md) | [English](1-Getting-Started.md) | [下一篇 →](2-Authentication_zh.md)

介绍文字。

## 1.1 安装

运行：

\`\`\`bash
npm install -g volclog
\`\`\`

## 1.2 配置

| 选项 | 说明 |
| --- | --- |
| --ak | 访问密钥 |
| --sk | 秘密密钥 |

参见 [2-Authentication_zh.md](2-Authentication_zh.md)。

---

[← 上一篇](../README_ZH.md) | [English](1-Getting-Started.md) | [下一篇 →](2-Authentication_zh.md)
`;

function validTree() {
  const files = {
    'README.md': `# volclog-cli

[中文版](README_ZH.md) | [English](README.md)

See [1-Getting-Started.md](docs/1-Getting-Started.md).
`,
    'README_ZH.md': `# volclog-cli

[中文版](README_ZH.md) | [English](README.md)

参见 [1-Getting-Started_zh.md](docs/1-Getting-Started_zh.md)。
`,
  };
  const names = [
    '1-Getting-Started',
    '2-Authentication',
    '3-Configuration',
    '4-Usage',
    '5-Practical-Guide',
    '6-Advanced',
    '7-Human-Shortcuts',
  ];
  // Navigation routes: [prev, pair, next] for each page index (0..6)
  const navEn = [
    ['../README.md', '1-Getting-Started_zh.md', '2-Authentication.md'],
    ['1-Getting-Started.md', '2-Authentication_zh.md', '3-Configuration.md'],
    ['2-Authentication.md', '3-Configuration_zh.md', '4-Usage.md'],
    ['3-Configuration.md', '4-Usage_zh.md', '5-Practical-Guide.md'],
    ['4-Usage.md', '5-Practical-Guide_zh.md', '6-Advanced.md'],
    ['5-Practical-Guide.md', '6-Advanced_zh.md', '7-Human-Shortcuts.md'],
    ['6-Advanced.md', '7-Human-Shortcuts_zh.md', '../README.md'],
  ];
  const navZh = [
    ['../README_ZH.md', '1-Getting-Started.md', '2-Authentication_zh.md'],
    ['1-Getting-Started_zh.md', '2-Authentication.md', '3-Configuration_zh.md'],
    ['2-Authentication_zh.md', '3-Configuration.md', '4-Usage_zh.md'],
    ['3-Configuration_zh.md', '4-Usage.md', '5-Practical-Guide_zh.md'],
    ['4-Usage_zh.md', '5-Practical-Guide.md', '6-Advanced_zh.md'],
    ['5-Practical-Guide_zh.md', '6-Advanced.md', '7-Human-Shortcuts_zh.md'],
    ['6-Advanced_zh.md', '7-Human-Shortcuts.md', '../README_ZH.md'],
  ];
  for (let i = 0; i < names.length; i++) {
    const n = names[i];
    const enNav = `[← Previous](${navEn[i][0]}) | [中文](${navEn[i][1]}) | [Next →](${navEn[i][2]})`;
    const zhNav = `[← 上一篇](${navZh[i][0]}) | [English](${navZh[i][1]}) | [下一篇 →](${navZh[i][2]})`;
    const page1EnNav = `[← Previous](../README.md) | [中文](1-Getting-Started_zh.md) | [Next →](2-Authentication.md)`;
    const page1ZhNav = `[← 上一篇](../README_ZH.md) | [English](1-Getting-Started.md) | [下一篇 →](2-Authentication_zh.md)`;
    const title = n.replace(/-/g, ' ');
    // Replace H1 title line, then replace both nav lines as full strings.
    let enBody = VALID_PAIR_EN
      .replace('# 1 Getting Started', `# ${title}`)
      .split(page1EnNav).join(enNav);
    let zhBody = VALID_PAIR_ZH
      .replace('# 1 快速开始', `# ${title}`)
      .split(page1ZhNav).join(zhNav);
    files[`docs/${n}.md`] = enBody;
    files[`docs/${n}_zh.md`] = zhBody;
  }
  return files;
}

// --- tests -----------------------------------------------------------------

test('valid complete seven-pair miniature tree passes', () => {
  const dir = makeTree(validTree());
  try {
    const diags = validateDocsTree(dir);
    assert.deepEqual(diags, [], `expected no diagnostics, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('missing root Chinese README fails', () => {
  const files = validTree();
  delete files['README_ZH.md'];
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('README_ZH.md')), `expected README_ZH.md diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('wrong README_CN.md is rejected', () => {
  const files = validTree();
  files['README_CN.md'] = '# bad';
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('README_CN.md')), `expected README_CN.md diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('missing numbered pair fails', () => {
  const files = validTree();
  delete files['docs/3-Configuration.md'];
  delete files['docs/3-Configuration_zh.md'];
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('3-Configuration')), `expected 3-Configuration diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('extra file and subdirectory under docs fails', () => {
  const files = validTree();
  files['docs/extra.md'] = '# extra';
  files['docs/subdir/inner.md'] = '# inner';
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('extra.md')), `expected extra.md diagnostic, got: ${diags.join('\n')}`);
    assert.ok(diags.some((d) => d.includes('subdir')), `expected subdir diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('mismatched heading level fails', () => {
  const files = validTree();
  files['docs/1-Getting-Started_zh.md'] = VALID_PAIR_ZH.replace('## 1.2 配置', '### 1.2 配置');
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('heading')), `expected heading diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('mismatched numeric prefix fails', () => {
  const files = validTree();
  files['docs/1-Getting-Started_zh.md'] = VALID_PAIR_ZH.replace('## 1.2 配置', '## 2.1 配置');
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('numeric') || d.includes('prefix')), `expected numeric prefix diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('mismatched fence language fails', () => {
  const files = validTree();
  files['docs/1-Getting-Started_zh.md'] = VALID_PAIR_ZH.replace('```bash', '```shell');
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('fence') || d.includes('code')), `expected fence diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('mismatched fence body fails', () => {
  const files = validTree();
  files['docs/1-Getting-Started_zh.md'] = VALID_PAIR_ZH.replace('npm install -g volclog', 'npm install volclog');
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('fence') || d.includes('code')), `expected fence body diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('mismatched table rows/columns fails', () => {
  const files = validTree();
  // Add an extra column to the ZH table
  files['docs/1-Getting-Started_zh.md'] = VALID_PAIR_ZH.replace(
    '| 选项 | 说明 |',
    '| 选项 | 说明 | 备注 |',
  ).replace(
    '| --- | --- |',
    '| --- | --- | --- |',
  ).replace(
    '| --ak | 访问密钥 |',
    '| --ak | 访问密钥 | a |',
  ).replace(
    '| --sk | 秘密密钥 |',
    '| --sk | 秘密密钥 | b |',
  );
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('table')), `expected table diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('broken relative file link fails', () => {
  const files = validTree();
  files['docs/1-Getting-Started.md'] = VALID_PAIR_EN.replace(
    '[2-Authentication.md](2-Authentication.md)',
    '[Missing](does-not-exist.md)',
  );
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('does-not-exist.md')), `expected broken link diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('missing anchor fails and valid Unicode anchor passes', () => {
  const files = validTree();
  // Target page has a heading "1.1 Install" -> slug "11-install"
  files['docs/1-Getting-Started.md'] = VALID_PAIR_EN.replace(
    '[2-Authentication.md](2-Authentication.md)',
    '[Install](#11-install)',
  );
  let dir = makeTree(files);
  try {
    let diags = validateDocsTree(dir);
    assert.deepEqual(diags, [], `valid anchor should pass, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }

  // Now a missing anchor
  files['docs/1-Getting-Started.md'] = VALID_PAIR_EN.replace(
    '[2-Authentication.md](2-Authentication.md)',
    '[Install](#no-such-heading)',
  );
  dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('no-such-heading')), `expected missing anchor diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('wrong-language neighbor link fails', () => {
  const files = validTree();
  // English page links to _zh neighbor (not its own pair)
  files['docs/1-Getting-Started.md'] = VALID_PAIR_EN.replace(
    '[2-Authentication.md](2-Authentication.md)',
    '[Auth](2-Authentication_zh.md)',
  );
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('language')), `expected language routing diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('allowed own-pair language switch passes', () => {
  const files = validTree();
  // English page links to its own _zh counterpart (language switch)
  files['docs/1-Getting-Started.md'] = VALID_PAIR_EN.replace(
    '[2-Authentication.md](2-Authentication.md)',
    '[中文](1-Getting-Started_zh.md)',
  );
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.deepEqual(diags, [], `own-pair language switch should pass, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('retired filenames are rejected', () => {
  const retired = [
    'README_CN.md',
    'authentication-zh.md',
    'cli-practical-guide.md',
    'cli-best-practices.md',
    'cli-human-shortcuts.md',
  ];
  for (const name of retired) {
    const files = validTree();
    if (name === 'README_CN.md') {
      files[name] = '# bad';
    } else {
      files[`docs/${name}`] = '# bad';
    }
    const dir = makeTree(files);
    try {
      const diags = validateDocsTree(dir);
      assert.ok(diags.some((d) => d.includes(name)), `expected retired ${name} diagnostic, got: ${diags.join('\n')}`);
    } finally {
      cleanup(dir);
    }
  }
});

test('forbidden references are rejected', () => {
  const forbidden = [
    'docs/plans',
    'docs/superpowers',
    'docs/verification',
    'docs/agentic-stage1',
    '.llm/internal-docs',
    '/home/',
    '/data00/',
    'volcengine-cli',
  ];
  for (const ref of forbidden) {
    const files = validTree();
    files['docs/1-Getting-Started.md'] = VALID_PAIR_EN + `\nSee ${ref}/foo for details.\n`;
    const dir = makeTree(files);
    try {
      const diags = validateDocsTree(dir);
      assert.ok(diags.some((d) => d.includes(ref)), `expected forbidden ${ref} diagnostic, got: ${diags.join('\n')}`);
    } finally {
      cleanup(dir);
    }
  }
});

test('diagnostics are deterministically ordered', () => {
  const files = validTree();
  delete files['README_ZH.md'];
  delete files['docs/3-Configuration.md'];
  delete files['docs/3-Configuration_zh.md'];
  files['docs/extra.md'] = '# extra';
  const dir = makeTree(files);
  try {
    const a = validateDocsTree(dir);
    const b = validateDocsTree(dir);
    assert.deepEqual(a, b, 'repeated calls must return identical ordering');
    const sorted = [...a].sort();
    assert.deepEqual(a, sorted, 'diagnostics must be sorted');
  } finally {
    cleanup(dir);
  }
});

// --- Gap 1: root README parity & language routing --------------------------

test('root README heading mismatch fails', () => {
  const files = validTree();
  files['README_ZH.md'] = `# volclog-cli

## 额外章节

[English](README.md)
`;
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('heading')), `expected heading diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('root README fence mismatch fails', () => {
  const files = validTree();
  files['README.md'] = `# volclog-cli

\`\`\`bash
echo hi
\`\`\`

[中文版](README_ZH.md)
`;
  files['README_ZH.md'] = `# volclog-cli

\`\`\`shell
echo hi
\`\`\`

[English](README.md)
`;
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('fence') || d.includes('code')), `expected fence diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('root README table mismatch fails', () => {
  const files = validTree();
  files['README.md'] = `# volclog-cli

| A | B |
| --- | --- |
| 1 | 2 |

[中文版](README_ZH.md)
`;
  files['README_ZH.md'] = `# volclog-cli

| A | B | C |
| --- | --- | --- |
| 1 | 2 | 3 |

[English](README.md)
`;
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('table')), `expected table diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('wrong-language root-to-numbered link fails', () => {
  const files = validTree();
  files['README.md'] = `# volclog-cli

[中文版](README_ZH.md)

See [auth](docs/2-Authentication_zh.md).
`;
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('language')), `expected language routing diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('allowed root own-pair switch passes', () => {
  const files = validTree();
  files['README.md'] = `# volclog-cli

[中文版](README_ZH.md)

See [start](docs/1-Getting-Started.md).
`;
  files['README_ZH.md'] = `# volclog-cli

[English](README.md)

参见 [start](docs/1-Getting-Started_zh.md)。
`;
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.deepEqual(diags, [], `root own-pair switch should pass, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

// --- Gap 2: reference-style links & path escaping --------------------------

test('reference-style link resolves and is validated', () => {
  const files = validTree();
  files['docs/1-Getting-Started.md'] = `# 1 Getting Started

[← Previous](../README.md) | [中文](1-Getting-Started_zh.md) | [Next →](2-Authentication.md)

Intro text.

## 1.1 Install

Run:

\`\`\`bash
npm install -g volclog
\`\`\`

## 1.2 Configure

| Option | Description |
| --- | --- |
| --ak | Access key |
| --sk | Secret key |

See [auth][authref].

[authref]: 2-Authentication.md

---

[← Previous](../README.md) | [中文](1-Getting-Started_zh.md) | [Next →](2-Authentication.md)
`;
  files['docs/1-Getting-Started_zh.md'] = `# 1 快速开始

[← 上一篇](../README_ZH.md) | [English](1-Getting-Started.md) | [下一篇 →](2-Authentication_zh.md)

介绍文字。

## 1.1 安装

运行：

\`\`\`bash
npm install -g volclog
\`\`\`

## 1.2 配置

| 选项 | 说明 |
| --- | --- |
| --ak | 访问密钥 |
| --sk | 秘密密钥 |

参见 [auth][authref].

[authref]: 2-Authentication_zh.md

---

[← 上一篇](../README_ZH.md) | [English](1-Getting-Started.md) | [下一篇 →](2-Authentication_zh.md)
`;
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.deepEqual(diags, [], `valid reference link should pass, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('nonexistent reference target fails', () => {
  const files = validTree();
  files['docs/1-Getting-Started.md'] = `# 1 Getting Started

See [missing][missref].

[missref]: does-not-exist.md
`;
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('does-not-exist.md')), `expected broken reference link diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('reference definition inside code fence is ignored', () => {
  const files = validTree();
  files['docs/1-Getting-Started.md'] = `# 1 Getting Started

[← Previous](../README.md) | [中文](1-Getting-Started_zh.md) | [Next →](2-Authentication.md)

Intro text.

## 1.1 Install

Run:

\`\`\`bash
[fake]: does-not-exist.md
\`\`\`

## 1.2 Configure

| Option | Description |
| --- | --- |
| --ak | Access key |
| --sk | Secret key |

See [auth][authref].

[authref]: 2-Authentication.md

---

[← Previous](../README.md) | [中文](1-Getting-Started_zh.md) | [Next →](2-Authentication.md)
`;
  files['docs/1-Getting-Started_zh.md'] = `# 1 快速开始

[← 上一篇](../README_ZH.md) | [English](1-Getting-Started.md) | [下一篇 →](2-Authentication_zh.md)

介绍文字。

## 1.1 安装

运行：

\`\`\`bash
[fake]: does-not-exist.md
\`\`\`

## 1.2 配置

| 选项 | 说明 |
| --- | --- |
| --ak | 访问密钥 |
| --sk | 秘密密钥 |

参见 [auth][authref].

[authref]: 2-Authentication_zh.md

---

[← 上一篇](../README_ZH.md) | [English](1-Getting-Started.md) | [下一篇 →](2-Authentication_zh.md)
`;
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.deepEqual(diags, [], `fenced definition should be ignored, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('path escaping repo root is rejected', () => {
  const dir = makeTree(validTree());
  // The outside file lives in the parent of the repo root, so it exists but
  // is lexically outside the repository. The checker must reject it on
  // containment grounds, not just "file not found".
  const outsideFile = join(dir, '..', 'outside.md');
  try {
    writeFileSync(outsideFile, '# outside', 'utf8');
    writeFileSync(
      join(dir, 'docs', '1-Getting-Started.md'),
      `# 1 Getting Started\n\nSee [outside](../../outside.md).\n`,
      'utf8',
    );

    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('outside') || d.includes('escape') || d.includes('root')), `expected escape diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
    rmSync(outsideFile, { force: true });
  }
});

test('valid tree passes when repository root has a symlinked ancestor', () => {
  const realParent = mkdtempSync(join(tmpdir(), 'check-docs-real-parent-'));
  const realRoot = join(realParent, 'repo');
  const aliasParent = `${realParent}-alias`;
  mkdirSync(realRoot);
  for (const [relPath, content] of Object.entries(validTree())) {
    const full = join(realRoot, relPath);
    mkdirSync(join(full, '..'), { recursive: true });
    writeFileSync(full, content, 'utf8');
  }
  symlinkSync(realParent, aliasParent, 'dir');

  try {
    const diags = validateDocsTree(join(aliasParent, 'repo'));
    assert.deepEqual(diags, [], `symlinked root ancestor should pass, got: ${diags.join('\n')}`);
  } finally {
    rmSync(aliasParent, { force: true });
    cleanup(realParent);
  }
});

test('missing repository root returns structured diagnostics without throwing', () => {
  const parent = mkdtempSync(join(tmpdir(), 'check-docs-missing-root-'));
  try {
    const diags = validateDocsTree(join(parent, 'missing'));
    assert.ok(Array.isArray(diags), 'should return diagnostics, not throw');
    assert.ok(diags.some((d) => d.includes('README.md')), `expected README diagnostic, got: ${diags.join('\n')}`);
    assert.ok(diags.some((d) => d.includes('docs/')), `expected docs diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(parent);
  }
});

test('escaping symlink is rejected', () => {
  const dir = makeTree(validTree());
  const outsideTarget = join(tmpdir(), `escape-target-${Date.now()}.md`);
  try {
    writeFileSync(outsideTarget, '# outside', 'utf8');
    symlinkSync(outsideTarget, join(dir, 'docs', 'escape.md'));
    writeFileSync(join(dir, 'docs', '1-Getting-Started.md'), `# 1 Getting Started\n\nSee [escape](escape.md).\n`, 'utf8');

    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('escape') || d.includes('outside') || d.includes('symlink') || d.includes('root')), `expected escaping symlink diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
    rmSync(outsideTarget, { force: true });
  }
});

// --- Gap 3: invalid entry types --------------------------------------------

test('expected docs path that is a directory returns diagnostic without throwing', () => {
  const dir = makeTree(validTree());
  try {
    rmSync(join(dir, 'docs', '1-Getting-Started.md'), { force: true });
    mkdirSync(join(dir, 'docs', '1-Getting-Started.md'), { recursive: true });

    const diags = validateDocsTree(dir);
    assert.ok(Array.isArray(diags), 'should return an array, not throw');
    assert.ok(diags.some((d) => d.includes('1-Getting-Started.md')), `expected diagnostic for directory path, got: ${diags.join('\n')}`);
    assert.deepEqual(diags, [...diags].sort(), 'diagnostics must be sorted');
  } finally {
    cleanup(dir);
  }
});

test('expected docs path that is a symlink returns diagnostic without throwing', () => {
  const dir = makeTree(validTree());
  try {
    rmSync(join(dir, 'docs', '1-Getting-Started.md'), { force: true });
    symlinkSync(join(dir, 'docs', '2-Authentication.md'), join(dir, 'docs', '1-Getting-Started.md'));

    const diags = validateDocsTree(dir);
    assert.ok(Array.isArray(diags), 'should return an array, not throw');
    assert.ok(diags.some((d) => d.includes('1-Getting-Started.md')), `expected diagnostic for symlink path, got: ${diags.join('\n')}`);
    assert.deepEqual(diags, [...diags].sort(), 'diagnostics must be sorted');
  } finally {
    cleanup(dir);
  }
});

test('root README that is a directory returns diagnostic without throwing', () => {
  const dir = makeTree(validTree());
  try {
    rmSync(join(dir, 'README.md'), { force: true });
    mkdirSync(join(dir, 'README.md'), { recursive: true });

    const diags = validateDocsTree(dir);
    assert.ok(Array.isArray(diags), 'should return an array, not throw');
    assert.ok(diags.some((d) => d.includes('README.md')), `expected diagnostic for directory README, got: ${diags.join('\n')}`);
    assert.deepEqual(diags, [...diags].sort(), 'diagnostics must be sorted');
  } finally {
    cleanup(dir);
  }
});

// --- Non-regular entry handling (symlinks/FIFOs/sockets) -------------------

test('extra symlink under docs is rejected', () => {
  const dir = makeTree(validTree());
  try {
    // A valid 14-file tree plus an extra symlink must not pass.
    symlinkSync(join(dir, 'docs', '1-Getting-Started.md'), join(dir, 'docs', 'extra-link.md'));
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('extra-link.md')), `expected diagnostic for extra symlink, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('retired docs filename as a symlink is rejected by name', () => {
  const dir = makeTree(validTree());
  try {
    // authentication-zh.md as a symlink must still trigger the retired-name rule.
    symlinkSync(join(dir, 'docs', '1-Getting-Started.md'), join(dir, 'docs', 'authentication-zh.md'));
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('authentication-zh.md') && d.includes('retired')), `expected retired-name diagnostic for symlink, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('root README_CN.md as a symlink is rejected by name', () => {
  const dir = makeTree(validTree());
  try {
    symlinkSync(join(dir, 'README.md'), join(dir, 'README_CN.md'));
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('README_CN.md')), `expected README_CN.md diagnostic for symlink, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

// --- Issue 1: docs/ as escaping symlink ------------------------------------

test('docs/ that is a symlink to an external tree is rejected', () => {
  const dir = mkdtempSync(join(tmpdir(), 'check-docs-'));
  const outsideDir = join(dir, '..', 'outside-docs');
  try {
    // Minimal valid root READMEs that do NOT link into docs/, so the only
    // potential violation is docs/ itself being a symlink.
    writeFileSync(join(dir, 'README.md'), `# volclog-cli\n\n[中文版](README_ZH.md)\n`, 'utf8');
    writeFileSync(join(dir, 'README_ZH.md'), `# volclog-cli\n\n[English](README.md)\n`, 'utf8');
    // Build a valid-looking 14-file tree outside the repo root with matching
    // EN/ZH content so pair/parity checks would pass if docs/ were real.
    mkdirSync(outsideDir, { recursive: true });
    const names = [
      '1-Getting-Started', '2-Authentication', '3-Configuration',
      '4-Usage', '5-Practical-Guide', '6-Advanced', '7-Human-Shortcuts',
    ];
    for (const n of names) {
      const body = `# 1 ${n}\n\n## 1.1 Section\n\nText.\n`;
      writeFileSync(join(outsideDir, `${n}.md`), body, 'utf8');
      writeFileSync(join(outsideDir, `${n}_zh.md`), body, 'utf8');
    }
    symlinkSync(outsideDir, join(dir, 'docs'));

    const diags = validateDocsTree(dir);
    assert.ok(
      diags.some((d) => d.includes('docs/') && (d.includes('symlink') || d.includes('real directory') || d.includes('not a') || d.includes('must be'))),
      `expected diagnostic about docs/ not being a real directory, got: ${diags.join('\n')}`,
    );
  } finally {
    cleanup(dir);
    rmSync(outsideDir, { recursive: true, force: true });
  }
});

// --- Issue 2: shortcut reference parsing -----------------------------------

test('full reference follows only its own definition, not shortcut scan', () => {
  const files = validTree();
  files['docs/1-Getting-Started.md'] = VALID_PAIR_EN.replace(
    'See [2-Authentication.md](2-Authentication.md).',
    'See [auth][ok].\n\n[auth]: missing.md\n[ok]: 2-Authentication.md',
  );
  files['docs/1-Getting-Started_zh.md'] = VALID_PAIR_ZH.replace(
    '参见 [2-Authentication_zh.md](2-Authentication_zh.md)。',
    '参见 [auth][ok].\n\n[auth]: missing.md\n[ok]: 2-Authentication_zh.md',
  );
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.deepEqual(diags, [], `full ref should follow only [ok], got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

// --- Navigation contract --------------------------------------------------

test('missing top navigation is rejected', () => {
  const files = validTree();
  files['docs/2-Authentication.md'] = files['docs/2-Authentication.md'].replace(
    '[← Previous](1-Getting-Started.md) | [中文](2-Authentication_zh.md) | [Next →](3-Configuration.md)\n\n',
    '',
  );
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(
      diags.some((d) => d.includes('docs/2-Authentication.md') && d.includes('missing top navigation')),
      `expected missing-top-navigation diagnostic, got: ${diags.join('\n')}`,
    );
  } finally {
    cleanup(dir);
  }
});

test('missing bottom navigation is rejected', () => {
  const files = validTree();
  const nav = '[← Previous](1-Getting-Started.md) | [中文](2-Authentication_zh.md) | [Next →](3-Configuration.md)';
  const content = files['docs/2-Authentication.md'];
  const bottomNav = content.lastIndexOf(nav);
  files['docs/2-Authentication.md'] =
    content.slice(0, bottomNav) + content.slice(bottomNav + nav.length);
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(
      diags.some((d) => d.includes('docs/2-Authentication.md') && d.includes('missing bottom navigation')),
      `expected missing-bottom-navigation diagnostic, got: ${diags.join('\n')}`,
    );
  } finally {
    cleanup(dir);
  }
});

test('wrong same-language previous or next navigation route is rejected', () => {
  const files = validTree();
  files['docs/3-Configuration.md'] = files['docs/3-Configuration.md'].replaceAll(
    '[← Previous](2-Authentication.md)',
    '[← Previous](1-Getting-Started.md)',
  );
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(
      diags.some((d) => d.includes('docs/3-Configuration.md') && d.includes('navigation links mismatch')),
      `expected wrong-route navigation diagnostic, got: ${diags.join('\n')}`,
    );
  } finally {
    cleanup(dir);
  }
});

test('navigation route with an anchor is not treated as the exact adjacent route', () => {
  const files = validTree();
  files['docs/3-Configuration.md'] = files['docs/3-Configuration.md'].replaceAll(
    '(2-Authentication.md)',
    '(2-Authentication.md#11-install)',
  );
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(
      diags.some((d) => d.includes('docs/3-Configuration.md') && d.includes('navigation links mismatch')),
      `expected anchored-route navigation diagnostic, got: ${diags.join('\n')}`,
    );
  } finally {
    cleanup(dir);
  }
});

// --- Issue 3: retired name in prose ----------------------------------------

test('retired filename in prose text is rejected', () => {
  const files = validTree();
  files['docs/1-Getting-Started.md'] = VALID_PAIR_EN + `\nSee also authentication-zh.md for legacy content.\n`;
  const dir = makeTree(files);
  try {
    const diags = validateDocsTree(dir);
    assert.ok(diags.some((d) => d.includes('authentication-zh.md')), `expected retired-name prose diagnostic, got: ${diags.join('\n')}`);
  } finally {
    cleanup(dir);
  }
});

test('CLI exits 0 on valid tree and nonzero on invalid tree', () => {
  const scriptPath = fileURLToPath(new URL('./check-docs.mjs', import.meta.url));

  const validDir = makeTree(validTree());
  try {
    const ok = spawnSync(process.execPath, [scriptPath, validDir], { encoding: 'utf8' });
    assert.equal(ok.status, 0, `valid tree should exit 0, got ${ok.status}\n${ok.stderr}`);
  } finally {
    cleanup(validDir);
  }

  const invalidDir = makeTree({ 'README.md': '# only root' });
  try {
    const bad = spawnSync(process.execPath, [scriptPath, invalidDir], { encoding: 'utf8' });
    assert.notEqual(bad.status, 0, `invalid tree should exit nonzero, got ${bad.status}`);
    assert.ok(bad.stdout.length > 0 || bad.stderr.length > 0, 'should print diagnostics');
  } finally {
    cleanup(invalidDir);
  }
});
