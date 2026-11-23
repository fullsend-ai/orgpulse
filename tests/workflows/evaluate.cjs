const vm = require('node:vm');
const fs = require('node:fs');
const request = JSON.parse(fs.readFileSync(0, 'utf8'));
const response = request.map(({script, globals}) => vm.runInNewContext(script, globals, {timeout: 1000}));
process.stdout.write(JSON.stringify(response));
