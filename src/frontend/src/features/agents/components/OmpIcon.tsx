import React from 'react'

/** Official Oh My Pi mark from https://omp.sh/favicon.svg */
export function OmpIcon({ className }: { className?: string }) {
  // Unique gradient id per mount so multiple icons on one page don't clash.
  const gradId = React.useId().replace(/:/g, '')
  return React.createElement(
    'svg',
    { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 64 64', className },
    React.createElement(
      'defs',
      null,
      React.createElement(
        'linearGradient',
        { id: gradId, x1: '0', y1: '0', x2: '1', y2: '1' },
        React.createElement('stop', { offset: '0', stopColor: '#ed4abf' }),
        React.createElement('stop', { offset: '.5', stopColor: '#9b4dff' }),
        React.createElement('stop', { offset: '1', stopColor: '#5ad8e6' }),
      ),
    ),
    React.createElement('rect', { width: '64', height: '64', rx: '12', fill: '#0f0a14' }),
    React.createElement('path', { fill: `url(#${gradId})`, d: 'M14 16h36v8H40v32h-8V24h-6v22h-8V24h-4z' }),
  )
}
