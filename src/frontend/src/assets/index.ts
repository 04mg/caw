const assetUrls = import.meta.glob<string>('@/assets/*.svg', {
  eager: true,
  query: '?url',
  import: 'default',
})

for (const url of Object.values(assetUrls)) {
  const image = new Image()
  image.src = url
}

export { assetUrls }
