import '@testing-library/jest-dom/vitest'

// jsdom does no layout: scrolling is a no-op here.
Element.prototype.scrollIntoView ??= function () {}
window.scrollTo = () => {}
