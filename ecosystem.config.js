const path = require('path');

// The application reads .env from cwd. Keep deployment secrets and PORT there.
module.exports = {
  apps: [{
    name: 'one-api',
    cwd: __dirname,
    script: path.join(__dirname, 'one-api'),
    interpreter: 'none',
    exec_mode: 'fork',
    instances: 1,
    args: ['--log-dir', './logs'],
    autorestart: true,
    restart_delay: 5000,
    watch: false,
    merge_logs: true,
    out_file: path.join(__dirname, 'logs/pm2-out.log'),
    error_file: path.join(__dirname, 'logs/pm2-error.log'),
    time: true,
  }],
};
