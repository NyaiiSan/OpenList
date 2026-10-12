import { onCleanup, onMount } from "solid-js"
import { getSetting } from "~/store"
import { base_path } from "~/utils"

const themeVariables = [
  "--openlist-theme-background-image",
  "--openlist-theme-background-position",
  "--openlist-theme-surface-color",
]

export const CustomTheme = () => {
  onMount(() => {
    const root = document.documentElement
    const previousThemeFlag = root.getAttribute("data-openlist-custom-theme")
    const previousVariables = new Map(
      themeVariables.map((name) => [name, root.style.getPropertyValue(name)]),
    )
    const themeStyle = document.createElement("style")
    document.head.append(themeStyle)
    const applyTheme = () => {
      const orientation = window.innerWidth < 768 ? "vertical" : "horizontal"
      const image = getSetting("custom_theme_background_image").trim()
      const imageURL =
        image.startsWith("/") && !image.startsWith("//")
          ? `${base_path}${image}`
          : image
      const position =
        getSetting(`custom_theme_background_position_${orientation}`).trim() ||
        "center"
      const color = getSetting("custom_theme_surface_color").trim()
      const blur = Number(getSetting("custom_theme_surface_blur"))
      const blurPixels = Number.isFinite(blur)
        ? Math.min(40, Math.max(0, blur))
        : 0

      root.style.setProperty(
        "--openlist-theme-background-image",
        imageURL ? `url("${imageURL.replace(/[\\"\r\n]/g, "\\$&")}")` : "none",
      )
      root.style.setProperty(
        "--openlist-theme-background-position",
        CSS.supports("background-position", position) ? position : "center",
      )
      root.style.setProperty(
        "--openlist-theme-surface-color",
        color && CSS.supports("color", color) ? color : "transparent",
      )
      themeStyle.textContent = `
        html[data-openlist-custom-theme="true"] .body > *,
        html[data-openlist-custom-theme="true"] .header,
        html[data-openlist-custom-theme="true"] .left-toolbar-box .left-toolbar,
        html[data-openlist-custom-theme="true"] .custom-theme-surface {
          backdrop-filter: blur(${blurPixels}px) !important;
        }
      `
      if (image || color || (Number.isFinite(blur) && blur > 0)) {
        root.setAttribute("data-openlist-custom-theme", "true")
      } else {
        root.removeAttribute("data-openlist-custom-theme")
      }
    }

    applyTheme()
    window.addEventListener("resize", applyTheme)
    onCleanup(() => {
      window.removeEventListener("resize", applyTheme)
      themeStyle.remove()
      if (previousThemeFlag === null) {
        root.removeAttribute("data-openlist-custom-theme")
      } else {
        root.setAttribute("data-openlist-custom-theme", previousThemeFlag)
      }
      previousVariables.forEach((value, name) => {
        if (value) root.style.setProperty(name, value)
        else root.style.removeProperty(name)
      })
    })
  })

  return null
}
