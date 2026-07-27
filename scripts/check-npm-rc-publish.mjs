const tag = String(process.env.npm_config_tag || '').trim();

if (tag !== 'rc') {
  console.error('release candidate packages must be published with --tag rc');
  process.exit(1);
}
