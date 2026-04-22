import '@testing-library/jest-dom'

// jsdom doesn't implement scrollTo — silence the noise from TaskForm's scroll-lock effect
window.scrollTo = () => {}
