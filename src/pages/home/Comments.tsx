import {
  Button,
  HStack,
  Text,
  Textarea,
  VStack,
  useColorModeValue,
} from "@hope-ui/solid"
import { For, Show, createSignal, onMount } from "solid-js"
import { useT } from "~/hooks"
import { getSettingBool } from "~/store"
import { hoverColor, r } from "~/utils"

type Comment = {
  id: number
  name: string
  content: string
  created_at: string
  is_guest?: boolean
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

const pageSize = 10

const maskIPv4 = (address: string): string | undefined => {
  const octets = address.split(".")
  if (
    octets.length !== 4 ||
    octets.some((octet) => !/^\d{1,3}$/.test(octet) || Number(octet) > 255)
  ) {
    return undefined
  }
  return `${octets[0]}.*.*.${octets[3]}`
}

const maskIPv6 = (address: string): string | undefined => {
  if (!address.includes(":") || !/^[\da-fA-F:]+$/.test(address)) {
    return undefined
  }

  const halves = address.split("::")
  if (halves.length > 2) return undefined
  const left = halves[0] ? halves[0].split(":") : []
  const right = halves.length === 2 && halves[1] ? halves[1].split(":") : []
  const omittedGroups = 8 - left.length - right.length
  if (
    (halves.length === 1 && omittedGroups !== 0) ||
    (halves.length === 2 && omittedGroups < 1)
  ) {
    return undefined
  }

  const groups = [
    ...left,
    ...Array.from({ length: omittedGroups }, () => "0"),
    ...right,
  ]
  if (groups.length !== 8) return undefined
  groups[1] = "*"
  groups[2] = "*"

  if (omittedGroups > 0) {
    const omittedStart = left.length
    const omittedEnd = omittedStart + omittedGroups - 1
    if (omittedStart <= 2 && omittedEnd >= 1) return groups.join(":")
    const maskedLeft = left.map((group, index) =>
      index === 1 || index === 2 ? "*" : group,
    )
    const maskedRight = right.map((group, index) => {
      const groupIndex = left.length + omittedGroups + index
      return groupIndex === 1 || groupIndex === 2 ? "*" : group
    })
    return `${maskedLeft.join(":")}::${maskedRight.join(":")}`
  }
  return groups.join(":")
}

const maskGuestIP = (address: string): string | undefined => {
  const ipv4 = maskIPv4(address)
  if (ipv4) return ipv4

  const mappedIPv4 = address.match(/^(.*:)(\d{1,3}(?:\.\d{1,3}){3})$/)
  if (mappedIPv4) {
    const masked = maskIPv4(mappedIPv4[2])
    if (masked) return `${mappedIPv4[1]}${masked}`
  }
  return maskIPv6(address)
}

const displayCommentName = (comment: Comment) => {
  const masked = maskGuestIP(comment.name)
  if (comment.is_guest || (comment.is_guest === undefined && masked)) {
    return masked ?? comment.name
  }
  return comment.name
}

export const Comments = () => {
  if (!getSettingBool("comments_enabled")) return null

  const t = useT()
  const cardBg = useColorModeValue("white", "$neutral3")
  const [comments, setComments] = createSignal<Comment[]>([])
  const [total, setTotal] = createSignal(0)
  const [offset, setOffset] = createSignal(0)
  const [content, setContent] = createSignal("")
  const [loading, setLoading] = createSignal(false)
  const [posting, setPosting] = createSignal(false)
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

  onMount(() => void loadComments())

  return (
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
          <Text fontSize="$sm" color="$neutral9">
            {total()}
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
            <Text fontSize="$xs" color="$neutral9">
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
            >
              <HStack
                justifyContent="space-between"
                alignItems="baseline"
                gap="$2"
              >
                <Text fontWeight="$semibold" class="comments-name">
                  {displayCommentName(comment)}
                </Text>
                <Text fontSize="$xs" color="$neutral9">
                  {new Date(comment.created_at).toLocaleString()}
                </Text>
              </HStack>
              <Text class="comments-content">{comment.content}</Text>
            </VStack>
          )}
        </For>
      </div>

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
  )
}
