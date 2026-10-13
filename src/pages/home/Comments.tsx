import {
  Button,
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
  ModalOverlay,
  HStack,
  Text,
  Textarea,
  VStack,
  useColorModeValue,
} from "@hope-ui/solid"
import { For, Show, createSignal, onCleanup, onMount } from "solid-js"
import { Portal } from "solid-js/web"
import "solid-contextmenu/dist/style.css"
import { useT, useUtil } from "~/hooks"
import { getMainColor, getSetting, getSettingBool, me } from "~/store"
import { UserMethods } from "~/types"
import { hoverColor, notify, r } from "~/utils"

type Comment = {
  id: number
  name: string
  content: string
  created_at: string
}

type CommentPage = {
  content: Comment[]
  total: number
}

type ApiResponse<T> = {
  code: number
  message: string
  data: T
}

type CommentMenuState = {
  x: number
  y: number
  comment: Comment
}

const pageSize = 10
type RGB = [number, number, number]

const parseColor = (value: string): RGB | undefined => {
  if (!value.trim()) return
  const canvas = document.createElement("canvas")
  const context = canvas.getContext("2d")
  if (!context) return

  context.fillStyle = "#000"
  context.fillStyle = value
  const normalized = context.fillStyle
  if (normalized.startsWith("#")) {
    let hex = normalized.slice(1)
    if (hex.length === 3 || hex.length === 4) {
      hex = [...hex].map((channel) => channel + channel).join("")
    }
    if (hex.length === 8) hex = hex.slice(0, 6)
    if (hex.length !== 6) return
    return [
      parseInt(hex.slice(0, 2), 16),
      parseInt(hex.slice(2, 4), 16),
      parseInt(hex.slice(4, 6), 16),
    ]
  }
  const channels = normalized.match(/^rgba?\(([^)]+)\)$/)?.[1]
  if (!channels) return
  const values = channels.split(/[\s,\/]+/).filter(Boolean)
  if (values.length < 3) return
  return values.slice(0, 3).map((channel) => {
    const number = parseFloat(channel)
    return channel.endsWith("%")
      ? Math.round((number / 100) * 255)
      : Math.round(number)
  }) as RGB
}

const colorAlpha = (value: string) => {
  const normalized = value.trim().toLowerCase()
  const hex = normalized.match(
    /^#(?:[\da-f]{3,4}|[\da-f]{6}|[\da-f]{8})$/i,
  )?.[0]
  if (hex && hex.length === 9) {
    return parseInt(hex.slice(-2), 16) / 255
  }
  if (hex && (hex.length === 5 || hex.length === 9)) {
    return parseInt(hex.slice(-1), 16) / 15
  }
  const rgb = normalized.match(/^rgba?\(([^)]+)\)$/)?.[1]
  if (!rgb) return 1
  const channels = rgb.split(/[\s,\/]+/).filter(Boolean)
  if (channels.length < 4) return 1
  const alpha = parseFloat(channels[3])
  return channels[3].endsWith("%") ? alpha / 100 : alpha
}

const compositeColor = (foreground: RGB, alpha: number, background: RGB): RGB =>
  foreground.map((channel, index) =>
    Math.round(channel * alpha + background[index] * (1 - alpha)),
  ) as RGB

const luminance = ([red, green, blue]: RGB) => {
  const linear = (channel: number) => {
    const normalized = channel / 255
    return normalized <= 0.04045
      ? normalized / 12.92
      : ((normalized + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * linear(red) + 0.7152 * linear(green) + 0.0722 * linear(blue)
}

const contrastRatio = (first: RGB, second: RGB) => {
  const a = luminance(first)
  const b = luminance(second)
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
}

const readableCommentMetaColor = (
  accentValue: string,
  surfaceValue: string,
  fallbackSurfaceValue: string,
) => {
  const accent = parseColor(accentValue)
  const baseSurface = parseColor(fallbackSurfaceValue) ?? [255, 255, 255]
  const surface = parseColor(surfaceValue)
  const panel = surface
    ? compositeColor(surface, colorAlpha(surfaceValue), baseSurface)
    : baseSurface
  if (accent && contrastRatio(accent, panel) >= 4.5) return accentValue
  const dark: RGB = [25, 25, 25]
  const light: RGB = [255, 255, 255]
  return contrastRatio(dark, panel) >= contrastRatio(light, panel)
    ? "rgb(25, 25, 25)"
    : "white"
}

export const Comments = () => {
  if (!getSettingBool("comments_enabled")) return null

  const t = useT()
  const cardBg = useColorModeValue("white", "$neutral3")
  const fallbackSurface = useColorModeValue("#ffffff", "#171923")
  const metaColor = () =>
    readableCommentMetaColor(
      getMainColor(),
      getSetting("custom_theme_surface_color"),
      fallbackSurface(),
    )
  const isAdmin = () => UserMethods.is_admin(me())
  const { copy } = useUtil()
  const [comments, setComments] = createSignal<Comment[]>([])
  const [total, setTotal] = createSignal(0)
  const [offset, setOffset] = createSignal(0)
  const [content, setContent] = createSignal("")
  const [loading, setLoading] = createSignal(false)
  const [posting, setPosting] = createSignal(false)
  const [deleting, setDeleting] = createSignal<number>()
  const [commentToDelete, setCommentToDelete] = createSignal<Comment>()
  const [commentMenu, setCommentMenu] = createSignal<CommentMenuState>()
  const [error, setError] = createSignal("")

  const loadComments = async () => {
    if (loading()) return
    setLoading(true)
    setError("")
    try {
      const response = (await r.get(
        `/public/comments?offset=${offset()}&limit=${pageSize}`,
      )) as ApiResponse<CommentPage>
      if (response.code !== 200) throw new Error(response.message)
      setComments((current) => [...current, ...response.data.content])
      setOffset((current) => current + response.data.content.length)
      setTotal(response.data.total)
    } catch (err) {
      setError(
        err instanceof Error ? err.message : t("home.comments.load_failed"),
      )
    } finally {
      setLoading(false)
    }
  }

  const postComment = async (event: SubmitEvent) => {
    event.preventDefault()
    const value = content().trim()
    if (!value || Array.from(value).length > 400 || posting()) return

    setPosting(true)
    setError("")
    try {
      const response = (await r.post("/public/comments", {
        content: value,
      })) as ApiResponse<Comment>
      if (response.code !== 200) throw new Error(response.message)
      setComments((current) => [response.data, ...current])
      setOffset((current) => current + 1)
      setTotal((current) => current + 1)
      setContent("")
    } catch (err) {
      setError(
        err instanceof Error ? err.message : t("home.comments.post_failed"),
      )
    } finally {
      setPosting(false)
    }
  }

  const deleteComment = async (comment: Comment) => {
    if (!isAdmin() || deleting() !== undefined) return

    setDeleting(comment.id)
    setError("")
    try {
      const response = (await r.delete(
        `/admin/comments/${comment.id}`,
      )) as ApiResponse<unknown>
      if (response.code !== 200) throw new Error(response.message)
      setComments((current) => current.filter((item) => item.id !== comment.id))
      setTotal((current) => Math.max(0, current - 1))
      notify.success(t("global.delete_success"))
      setCommentToDelete(undefined)
    } catch (err) {
      setError(
        err instanceof Error ? err.message : t("home.comments.delete_failed"),
      )
    } finally {
      setDeleting(undefined)
    }
  }

  onMount(() => void loadComments())

  const showCommentMenu = (event: MouseEvent, comment: Comment) => {
    event.preventDefault()
    const menuHeight = isAdmin() ? 80 : 46
    setCommentMenu({
      x: Math.max(8, Math.min(event.clientX, window.innerWidth - 208)),
      y: Math.max(8, Math.min(event.clientY, window.innerHeight - menuHeight)),
      comment,
    })
  }

  const handleOutsideClick = (event: MouseEvent) => {
    if (
      event.target instanceof Element &&
      !event.target.closest(".comments-context-menu")
    ) {
      setCommentMenu(undefined)
    }
  }

  const handleOutsideContextMenu = (event: MouseEvent) => {
    if (
      event.target instanceof Element &&
      !event.target.closest(".comments-item")
    ) {
      setCommentMenu(undefined)
    }
  }

  onMount(() => {
    window.addEventListener("click", handleOutsideClick)
    window.addEventListener("contextmenu", handleOutsideContextMenu)
    onCleanup(() => {
      window.removeEventListener("click", handleOutsideClick)
      window.removeEventListener("contextmenu", handleOutsideContextMenu)
    })
  })

  const openDeleteConfirmation = (comment: Comment) => {
    setCommentMenu(undefined)
    setError("")
    setCommentToDelete(comment)
  }

  const closeDeleteConfirmation = () => {
    if (deleting() !== undefined) return
    setCommentToDelete(undefined)
    setError("")
  }

  return (
    <>
      <VStack
        class="comments"
        alignItems="stretch"
        w="$full"
        rounded="$xl"
        p="$4"
        bgColor={cardBg()}
        shadow="$lg"
        gap="$3"
      >
        <HStack justifyContent="space-between" alignItems="baseline">
          <Text as="h2" fontSize="$lg" fontWeight="$semibold">
            {t("home.comments.title")}
          </Text>
          <Show when={total() > 0}>
            <Text fontSize="$sm" color={metaColor()}>
              {t("home.comments.count", { count: total() })}
            </Text>
          </Show>
        </HStack>

        <form class="comments-form" onSubmit={postComment}>
          <VStack alignItems="stretch" gap="$2">
            <Textarea
              value={content()}
              onInput={(event) => setContent(event.currentTarget.value)}
              maxLength={800}
              rows={3}
              placeholder={t("home.comments.content_placeholder")}
              aria-label={t("home.comments.content_placeholder")}
            />
            <HStack justifyContent="space-between" alignItems="center">
              <Text fontSize="$xs" color={metaColor()}>
                {Array.from(content()).length}/400
              </Text>
              <Button
                type="submit"
                size="sm"
                disabled={
                  loading() ||
                  posting() ||
                  !content().trim() ||
                  Array.from(content().trim()).length > 400
                }
              >
                {posting()
                  ? t("home.comments.posting")
                  : t("home.comments.submit")}
              </Button>
            </HStack>
          </VStack>
        </form>

        <Show when={error()}>
          <Text class="comments-error" role="alert">
            {error()}
          </Text>
        </Show>
        <Show when={comments().length === 0 && !loading() && !error()}>
          <Text color="$neutral9">{t("home.comments.empty")}</Text>
        </Show>

        <div class="comments-list">
          <For each={comments()}>
            {(comment) => (
              <VStack
                as="article"
                class="comments-item"
                alignItems="stretch"
                w="$full"
                gap="$2"
                p="$2"
                rounded="$lg"
                transition="all 0.3s"
                _hover={{
                  transform: "scale(1.01)",
                  bgColor: hoverColor(),
                }}
                onContextMenu={(event: MouseEvent) =>
                  showCommentMenu(event, comment)
                }
              >
                <HStack
                  justifyContent="space-between"
                  alignItems="baseline"
                  gap="$2"
                >
                  <Text fontWeight="$semibold" class="comments-name">
                    {comment.name}
                  </Text>
                  <Text fontSize="$xs" color={metaColor()}>
                    {new Date(comment.created_at).toLocaleString()}
                  </Text>
                </HStack>
                <Text class="comments-content">{comment.content}</Text>
              </VStack>
            )}
          </For>
        </div>

        <Portal>
          <Show when={commentMenu()}>
            {(menu) => (
              <div
                class="solid-contextmenu comments-context-menu"
                style={{
                  left: `${menu().x}px`,
                  top: `${menu().y}px`,
                }}
                role="menu"
              >
                <div
                  class="solid-contextmenu__item"
                  role="menuitem"
                  tabIndex={0}
                  onClick={(event) => {
                    event.stopPropagation()
                    setCommentMenu(undefined)
                    void copy(menu().comment.content)
                  }}
                >
                  <div class="solid-contextmenu__item__content">
                    {t("home.comments.copy")}
                  </div>
                </div>
                <Show when={isAdmin()}>
                  <div
                    class="solid-contextmenu__item"
                    role="menuitem"
                    tabIndex={0}
                    onClick={(event) => {
                      event.stopPropagation()
                      openDeleteConfirmation(menu().comment)
                    }}
                  >
                    <div class="solid-contextmenu__item__content">
                      {t("global.delete")}
                    </div>
                  </div>
                </Show>
              </div>
            )}
          </Show>
        </Portal>

        <Show when={offset() < total()}>
          <Button
            size="sm"
            variant="outline"
            onClick={() => void loadComments()}
            disabled={loading()}
          >
            {loading()
              ? t("home.comments.loading")
              : t("home.comments.load_more")}
          </Button>
        </Show>
        <Show when={comments().length === 0 && error()}>
          <Button
            size="sm"
            variant="outline"
            onClick={() => void loadComments()}
            disabled={loading()}
          >
            {t("home.comments.retry")}
          </Button>
        </Show>
      </VStack>

      <Modal
        blockScrollOnMount={false}
        opened={commentToDelete() !== undefined}
        onClose={closeDeleteConfirmation}
        size={{
          "@initial": "xs",
          "@md": "md",
        }}
      >
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{t("global.delete")}</ModalHeader>
          <ModalBody>
            <p>
              {t("home.comments.delete_confirm", {
                name: commentToDelete() ? commentToDelete()!.name : "",
              })}
            </p>
            <Show when={error()}>
              <Text class="comments-error" role="alert">
                {error()}
              </Text>
            </Show>
          </ModalBody>
          <ModalFooter display="flex" gap="$2">
            <Button
              onClick={closeDeleteConfirmation}
              colorScheme="neutral"
              disabled={deleting() !== undefined}
            >
              {t("global.cancel")}
            </Button>
            <Button
              colorScheme="danger"
              loading={deleting() === commentToDelete()?.id}
              disabled={deleting() !== undefined}
              onClick={() => {
                const comment = commentToDelete()
                if (comment) void deleteComment(comment)
              }}
            >
              {t("global.confirm")}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </>
  )
}
