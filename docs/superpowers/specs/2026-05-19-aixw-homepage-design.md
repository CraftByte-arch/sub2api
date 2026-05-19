# Aixw Homepage Design Spec

## 1. Overview
The goal is to implement a new minimalist landing page based on the provided visual mockup, using pure code. We will create a brand new Vue component for this and route the root path (`/`) to it, preserving the original `HomeView.vue` for future reference.

## 2. Architecture & Components
- **New View Component**: Create `src/views/public/AixwHomeView.vue`.
- **Router Update**: Modify `src/router/index.ts`. Change the component import for the `/` route to point to `AixwHomeView.vue`.

## 3. Visual Layout & Styling
The layout will use TailwindCSS for responsive and semantic styling.

- **Background**: Full screen (`h-screen w-screen`), light gradient or off-white background (`bg-gray-50/30` or similar).
- **Logo (Top Left)**: 
  - Absolute positioning (`absolute top-8 left-12`).
  - Text: `A AIXW`. The `A` can be stylized with a small SVG or pure CSS triangle to match the brand mark.
- **Center Content**: 
  - Absolute centering (`absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2`).
  - Text: `Move faster.` (Large font size, e.g., `text-6xl`, dark color, tracking tight).
  - Button: `Get started ->` (Black background `bg-black`, white text, rounded corners, padding, hover effects). Clicking this routes to `/login`.
- **Bottom Navigation/Labels**: 
  - Absolute positioning (`absolute bottom-8 left-0 w-full`).
  - Flex container with space-around (`flex justify-around`).
  - Items: `RELIABLE`, `GLOBAL`, `MINIMAL`, `FAST`.
  - Style: small text, gray color, uppercase, letter spacing (`text-xs text-gray-400 tracking-[0.2em]`).
  - A subtle border line on top or center-aligned behind the text.

## 4. 3D Graphic Implementation (Pure Code SVG)
- **Positioning**: Fixed to the right side (`absolute right-0 top-1/2 -translate-y-1/2 translate-x-1/4`).
- **Implementation**: Inline `<svg>` element within the template.
- **Visuals**:
  - We will use 3-4 overlapping `<path>` elements to simulate a folded origami/glass structure.
  - Each path will use an `<linearGradient>` with varying opacities to recreate the light and shadow reflections shown in the mockup.
  - The SVG will be set to `pointer-events-none` so it doesn't block interactions with the rest of the page.

## 5. Scope & Tradeoffs
- **Scope**: Focused strictly on the UI implementation of the landing page and routing update. 
- **Tradeoffs**: The pure code SVG approach allows for infinite scaling and zero network overhead for image fetching, but might require minor manual visual tweaking to perfectly mimic the complex 3D refractions of the original image.

## 6. Testing
- Run Vite local dev server to ensure the page renders correctly.
- Verify that navigating to `/` loads the new `AixwHomeView.vue`.
- Verify the "Get started" button correctly redirects to `/login`.
- Ensure responsive behavior (the center content stays centered, and SVG doesn't break mobile views; likely need to hide or scale down the SVG on very small screens).
