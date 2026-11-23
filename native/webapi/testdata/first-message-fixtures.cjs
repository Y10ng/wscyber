// 用工作区现有 TypeScript 编译器执行前端实际纯函数；不加载 React、不访问网络。
// node native/webapi/testdata/first-message-fixtures.cjs <typescript目录> [--write]
// 默认核对夹具，--write 仅用于文案变更后重新生成。
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const ts = require(path.resolve(process.argv[2]))
const root = path.resolve(__dirname, '../../ui/source-overlay/src')
const functions = [
  ['hooks/useChatSession.ts', ['webApiFirstMessage', 'webApiVerificationFirstMessage']],
  ['lib/retest-message.ts', ['buildWebApiRetestFirstMessage']],
  ['lib/scope-message.ts', ['scopeMessage', 'scopeRequirement']],
  ['lib/credential-message.ts', ['buildCredentialMessageLines', 'appendCredentialMessage']],
]
let code = ''
for (const [file, names] of functions) {
  const text = fs.readFileSync(path.join(root, file), 'utf8')
  const parsed = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true)
  for (const name of names) {
    const declaration = parsed.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name)
    if (!declaration) throw new Error(`missing function ${name} in ${file}`)
    code += declaration.getText(parsed).replace(/^export /, '') + '\n'
  }
}
const compiled = ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
const context = vm.createContext({})
vm.runInContext(compiled, context)
const origins = ['http://example.test', 'https://api.example.test']
const source = {
  number: 'WSC-000042',
  content: { title: '合成接口候选', description: '中性测试描述。', steps: [{ text: '读取合成接口' }], remediation: '核对访问控制。', limitations: '仅合成测试。' },
  evidence: [{ method: 'GET', url: 'https://api.example.test/items/1' }, { url: 'https://api.example.test/items/2' }],
}
const credentials = context.buildCredentialMessageLines([{ id: 'account-a', label: '测试账号', username: 'user-a', has_secret: true }])
const fixtures = []
for (const scope of ['origins', 'unrestricted']) {
  const task = { known_info: '合成目标', objective: '核对合成接口', origins, policy: { scope } }
  for (const [kind, message] of [
    ['research', context.webApiFirstMessage(task)],
    ['verification', context.webApiVerificationFirstMessage(origins, source, scope)],
    ['retest', context.buildWebApiRetestFirstMessage(origins, source, scope)],
  ]) fixtures.push({ scope, kind, message: context.appendCredentialMessage(message, credentials) })
}
const output = JSON.stringify(fixtures, null, 2) + '\n'
const fixturePath = path.join(__dirname, 'first-message-fixtures.json')
if (process.argv.includes('--write')) fs.writeFileSync(fixturePath, output)
else if (fs.readFileSync(fixturePath, 'utf8') !== output) throw new Error('frontend first-message fixtures are stale')
console.log(`PASS: ${fixtures.length} frontend first-message fixtures (${process.argv.includes('--write') ? 'written' : 'matched'})`)
