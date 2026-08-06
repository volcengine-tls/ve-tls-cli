const version = String(process.env.npm_package_version || '').trim();
const configuredTag = String(
  process.env.npm_config_tag || process.env.npm_package_publishConfig_tag || '',
).trim();
const tag = configuredTag || 'latest';

if (!version) {
  console.error('npm package version is required for publish validation');
  process.exit(1);
}

const expectedTag = version.includes('-') ? 'rc' : 'latest';
if (tag !== expectedTag) {
  console.error(`${version} must be published with --tag ${expectedTag}`);
  process.exit(1);
}
